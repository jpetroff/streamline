// Package configuration stores portable, named viewer configurations as JSON files.
package configuration

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"streamline/internal/query"
)

const MaxBytes = 64 << 10

// Document is the complete versioned on-disk format. IDs are filenames, not data.
type Document struct {
	Version int      `json:"version"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Mode    string   `json:"mode"`
	Columns []Column `json:"columns"`
	Filters Filters  `json:"filters"`
}
type Column struct {
	Path       string   `json:"path"`
	DateFormat string   `json:"dateFormat"`
	Width      *float64 `json:"width,omitempty"`
}
type Filters struct {
	Filter query.FilterList  `json:"filter"`
	Search *query.SearchSpec `json:"search"`
}
type Issue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Index   int    `json:"index,omitempty"`
	Line    int    `json:"line,omitempty"`
}
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string { return "Check the configuration fields." }

type Entry struct {
	ID       string   `json:"id"`
	Document Document `json:"document"`
}
type FileError struct {
	File    string  `json:"file"`
	Message string  `json:"message"`
	Issues  []Issue `json:"issues,omitempty"`
}
type Listing struct {
	Directory string      `json:"directory"`
	Entries   []Entry     `json:"entries"`
	Errors    []FileError `json:"errors"`
}

// Decode requires one complete document and rejects unknown or missing fields.
func Decode(data []byte) (Document, error) {
	var doc Document
	fail := func(message string) (Document, error) {
		return doc, &ValidationError{[]Issue{{Field: "document", Message: message}}}
	}
	if len(data) > MaxBytes {
		return fail("Configuration must be at most 64 KiB.")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return fail("Invalid configuration JSON: " + err.Error())
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fail("Enter exactly one JSON document.")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fail("Configuration must be a JSON object.")
	}
	for _, key := range []string{"version", "name", "command", "mode", "columns", "filters"} {
		if value, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fail("Missing configuration field: " + key)
		}
	}
	var nested struct {
		Columns []map[string]json.RawMessage `json:"columns"`
		Filters struct {
			Search map[string]json.RawMessage `json:"search"`
		} `json:"filters"`
	}
	if err := json.Unmarshal(data, &nested); err != nil {
		return fail("Invalid filters and search object.")
	}
	for i, column := range nested.Columns {
		if width, ok := column["width"]; ok && bytes.Equal(bytes.TrimSpace(width), []byte("null")) {
			return doc, &ValidationError{[]Issue{{Field: "columns", Index: i + 1, Message: "width must be a finite number of at least 144 pixels."}}}
		}
	}
	for _, key := range []string{"text", "mode", "operator"} {
		value, ok := nested.Filters.Search[key]
		var text string
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &text) != nil {
			return fail("search." + key + " must be a string.")
		}
	}
	return doc, Validate(doc)
}

// Validate compiles expressions without creating queries or executing commands.
func Validate(doc Document) error {
	issues := []Issue{}
	add := func(field, message string) { issues = append(issues, Issue{Field: field, Message: message}) }
	if doc.Version != 1 {
		add("version", "Unsupported configuration version; expected 1.")
	}
	if strings.TrimSpace(doc.Name) == "" {
		add("name", "Enter a name.")
	}
	if doc.Mode != "auto" && doc.Mode != "text" {
		add("mode", "Choose auto or text.")
	}
	if len(doc.Columns) == 0 {
		add("columns", "Enter at least one column.")
	}
	for i, column := range doc.Columns {
		if strings.TrimSpace(column.Path) == "" {
			issues = append(issues, Issue{Field: "columns", Index: i + 1, Message: "Enter a nonempty column path."})
		}
		if column.Width != nil && (math.IsNaN(*column.Width) || math.IsInf(*column.Width, 0) || *column.Width < 144) {
			issues = append(issues, Issue{Field: "columns", Index: i + 1, Message: "width must be a finite number of at least 144 pixels."})
		}
		switch column.DateFormat {
		case "original", "iso", "local", "date", "time":
		default:
			issues = append(issues, Issue{Field: "columns", Index: i + 1, Message: "Choose a supported dateFormat."})
		}
	}
	if doc.Filters.Filter == nil {
		add("filters", "filter must be a JSON array.")
	}
	if _, err := (query.TupleCompiler{}).Compile(doc.Filters.Filter); err != nil {
		for _, issue := range query.AsAPIError(err).FilterErrors {
			issues = append(issues, Issue{Field: "filters", Index: issue.Index, Message: issue.Property + ": " + issue.Message})
		}
	}
	search := doc.Filters.Search
	if search == nil {
		add("filters", "search must contain text, mode, and operator.")
	} else {
		if search.Mode != "plain" && search.Mode != "regexp" {
			add("filters", "Search mode must be plain or regexp.")
		}
		if search.Operator != "and" && search.Operator != "or" {
			add("filters", "Search operator must be and or or.")
		}
		if err := query.ValidateSearch(search); err != nil {
			apiErr := query.AsAPIError(err)
			for _, issue := range apiErr.LineErrors {
				issues = append(issues, Issue{Field: "filters", Line: issue.Line, Message: issue.Message})
			}
		}
	}
	if len(issues) > 0 {
		return &ValidationError{issues}
	}
	return nil
}

var unsafeName = regexp.MustCompile(`[^a-z0-9]+`)
var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

// Store opens the directory per operation so filesystem errors cannot prevent viewing logs.
type Store struct {
	Directory string
	mu        sync.Mutex
}

func New(directory string) *Store { return &Store{Directory: directory} }
func (s *Store) open() (*os.Root, error) {
	if err := os.MkdirAll(s.Directory, 0700); err != nil {
		return nil, err
	}
	base, err := os.OpenRoot(s.Directory)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	if err := base.Mkdir("configs", 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	info, err := base.Lstat("configs")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("configs must be a directory, not a symbolic link")
	}
	return base.OpenRoot("configs")
}
func read(root *os.Root, id string) (Document, error) {
	if !safeID.MatchString(id) {
		return Document{}, fs.ErrNotExist
	}
	info, err := root.Lstat(id + ".json")
	if err != nil {
		return Document{}, err
	}
	if !info.Mode().IsRegular() {
		return Document{}, fmt.Errorf("configuration must be a regular file")
	}
	file, err := root.Open(id + ".json")
	if err != nil {
		return Document{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		return Document{}, err
	}
	return Decode(data)
}
func (s *Store) List() (Listing, error) {
	result := Listing{Directory: s.Directory, Entries: []Entry{}, Errors: []FileError{}}
	root, err := s.open()
	if err != nil {
		return result, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return result, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		doc, err := read(root, id)
		if err != nil {
			item := FileError{File: entry.Name(), Message: err.Error()}
			var invalid *ValidationError
			if errors.As(err, &invalid) {
				item.Issues = invalid.Issues
			}
			result.Errors = append(result.Errors, item)
			continue
		}
		result.Entries = append(result.Entries, Entry{ID: id, Document: doc})
	}
	return result, nil
}
func (s *Store) Get(id string) (Entry, error) {
	root, err := s.open()
	if err != nil {
		return Entry{}, err
	}
	defer root.Close()
	doc, err := read(root, id)
	return Entry{ID: id, Document: doc}, err
}
func randomID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

// filenameName keeps generated IDs portable and within the 128-character limit.
func filenameName(name string) string {
	name = unsafeName.ReplaceAllString(strings.ToLower(name), "-")
	if len(name) > 95 {
		name = name[:95]
	}
	name = strings.Trim(name, "-")
	if name == "" {
		return "configuration"
	}
	return name
}

// Delete only removes valid saved entries, under the same lock used by Save.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.open()
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err := read(root, id); err != nil {
		return err
	}
	return root.Remove(id + ".json")
}

func (s *Store) Save(id string, doc Document) (Entry, error) {
	if err := Validate(doc); err != nil {
		return Entry{}, err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return Entry{}, err
	}
	data = append(data, '\n')
	if len(data) > MaxBytes {
		return Entry{}, &ValidationError{[]Issue{{Field: "document", Message: "Configuration must be at most 64 KiB when formatted."}}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := s.open()
	if err != nil {
		return Entry{}, err
	}
	defer root.Close()
	if id == "" {
		id, err = randomID()
		id = filenameName(doc.Name) + "-" + id
	} else {
		_, err = read(root, id)
	}
	if err != nil {
		return Entry{}, err
	}
	if err := atomicWrite(root, id+".json", data); err != nil {
		return Entry{}, err
	}
	return Entry{ID: id, Document: doc}, nil
}

func atomicWrite(root *os.Root, name string, data []byte) error {
	temporary, err := randomID()
	if err != nil {
		return err
	}
	temporary = "." + temporary + ".tmp"
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := root.Rename(temporary, name); err != nil {
		return err
	}
	return nil
}

// ResolveDirectory makes all overrides absolute without relying on platform config conventions.
func ResolveDirectory(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	return defaultDirectory()
}
