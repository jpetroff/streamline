package configuration

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"streamline/internal/query"
)

func sample() Document {
	return Document{Version: 1, Name: "Errors", Command: "  printf 'hello\\n'\n", Mode: "auto", Columns: []Column{{Path: "timestamp", DateFormat: "iso"}, {Path: "message", DateFormat: "original"}}, Filters: Filters{Filter: query.FilterList{{Field: "level", Op: "regex", Value: "error|fatal"}}, Search: &query.SearchSpec{Text: "timeout\n\nretry ", Mode: "regexp", Operator: "or"}}}
}
func TestRoundTripCloneTransferAndExternalEdits(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "settings")
	store := New(directory)
	original, err := store.Save("", sample())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "configs", original.ID+".json")
	bytes, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(bytes), "\n  \"version\": 1") {
		t.Fatalf("formatted file: %s %v", bytes, err)
	}
	for path, mode := range map[string]fs.FileMode{directory: 0700, filepath.Dir(file): 0700, file: 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("permissions %s: %v %v", path, info, err)
		}
	}
	restored, err := New(directory).Get(original.ID)
	if err != nil || !reflect.DeepEqual(original, restored) {
		t.Fatalf("round trip: %#v %v", restored, err)
	}
	cloneDoc := restored.Document
	cloneDoc.Name = "Errors copy"
	clone, err := store.Save("", cloneDoc)
	if err != nil || clone.ID == original.ID {
		t.Fatalf("clone: %#v %v", clone, err)
	}
	cloneDoc.Command = "updated"
	if _, err := store.Save(clone.ID, cloneDoc); err != nil {
		t.Fatal(err)
	}
	restored, _ = store.Get(original.ID)
	if restored.Document.Command != sample().Command {
		t.Fatal("clone changed original")
	}
	destination := New(t.TempDir())
	if _, err := destination.List(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination.Directory, "configs", "copied.json"), bytes, 0600); err != nil {
		t.Fatal(err)
	}
	copied, err := destination.Get("copied")
	if err != nil || !reflect.DeepEqual(copied.Document, original.Document) {
		t.Fatalf("transfer: %#v %v", copied, err)
	}
	copied.Document.Name = "Edited outside Streamline"
	edited, _ := json.Marshal(copied.Document)
	if err := os.WriteFile(filepath.Join(destination.Directory, "configs", "copied.json"), edited, 0600); err != nil {
		t.Fatal(err)
	}
	list, err := destination.List()
	if err != nil || len(list.Entries) != 1 || list.Entries[0].Document.Name != copied.Document.Name {
		t.Fatalf("external edit: %#v %v", list, err)
	}
}
func TestMalformedFilesAreIsolatedAndNeverRewritten(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Save("", sample()); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"broken": "{", "future": strings.Replace(string(marshal(t, sample())), `"version":1`, `"version":2`, 1), "oversized": strings.Repeat(" ", MaxBytes+1)} {
		if err := os.WriteFile(filepath.Join(store.Directory, "configs", name+".json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.List()
	if err != nil || len(list.Entries) != 1 || len(list.Errors) != 3 {
		t.Fatalf("listing: %#v %v", list, err)
	}
	if _, err := store.Save("future", sample()); err == nil {
		t.Fatal("rewrote future version")
	}
	data, _ := os.ReadFile(filepath.Join(store.Directory, "configs", "future.json"))
	if !strings.Contains(string(data), `"version":2`) {
		t.Fatal("future file was changed")
	}
}
func marshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestValidation(t *testing.T) {
	for name, mutate := range map[string]func(*Document){
		"version": func(d *Document) { d.Version = 2 }, "name": func(d *Document) { d.Name = " " }, "mode": func(d *Document) { d.Mode = "bad" },
		"columns": func(d *Document) { d.Columns = nil }, "date format": func(d *Document) { d.Columns[0].DateFormat = "bad" },
		"filter array": func(d *Document) { d.Filters.Filter = nil }, "filter regex": func(d *Document) { d.Filters.Filter[0].Value = "[" },
		"search syntax": func(d *Document) { d.Filters.Search.Text = "valid\n\n[" }, "JS-only": func(d *Document) { d.Filters.Search.Text = "(?=lookahead)" },
	} {
		t.Run(name, func(t *testing.T) {
			doc := sample()
			mutate(&doc)
			var invalid *ValidationError
			if !errors.As(Validate(doc), &invalid) || len(invalid.Issues) == 0 {
				t.Fatal("expected validation issues")
			}
		})
	}
	doc := sample()
	doc.Command = ""
	doc.Filters.Search.Text = `(?i)hello\z`
	if err := Validate(doc); err != nil {
		t.Fatalf("Go-only expression and empty command: %v", err)
	}
	data := string(marshal(t, sample()))
	for _, invalid := range []string{data + "{}", strings.Replace(data, `"name":"Errors",`, "", 1), strings.Replace(data, `"command":`, `"unknown":`, 1), strings.Replace(data, `"text":`, `"unknown":`, 1), strings.Replace(data, `"text":"timeout\n\nretry "`, `"text":null`, 1)} {
		if _, err := Decode([]byte(invalid)); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
}
func TestConfinementAndWriteFailure(t *testing.T) {
	store := New(t.TempDir())
	entry, err := store.Save("", sample())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../outside", "/tmp/file", "a/b", "..", ""} {
		if _, err := store.Get(id); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("unsafe ID %q: %v", id, err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, marshal(t, sample()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(store.Directory, "configs", "link.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("link"); err == nil {
		t.Fatal("followed symlink")
	}
	root, err := store.open()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("blocked.json", 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(root, "blocked.json", []byte("new")); err == nil {
		t.Fatal("expected failed rename")
	}
	files, _ := os.ReadDir(filepath.Join(store.Directory, "configs"))
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".tmp") {
			t.Fatal("temporary file leaked")
		}
	}
	doc := sample()
	doc.Command = strings.Repeat("x", MaxBytes)
	if _, err := store.Save(entry.ID, doc); err == nil {
		t.Fatal("oversized update accepted")
	}
	restored, err := store.Get(entry.ID)
	if err != nil || restored.Document.Command != sample().Command {
		t.Fatal("failed write changed prior entry")
	}
	blocked := New(outside)
	if _, err := blocked.List(); err == nil {
		t.Fatal("expected storage failure")
	}
}
func TestDirectoryOverride(t *testing.T) {
	got, err := ResolveDirectory("relative-settings")
	want, _ := filepath.Abs("relative-settings")
	if err != nil || got != want {
		t.Fatalf("override %q %v", got, err)
	}
}

func TestNamedFilenames(t *testing.T) {
	store := New(t.TempDir())
	for name, prefix := range map[string]string{
		"Service Errors / production": "service-errors-production",
		"../../ My config!":           "my-config",
		"🔥 / 日本語":                     "configuration",
		strings.Repeat("A", 200):      strings.Repeat("a", 95),
	} {
		doc := sample()
		doc.Name = name
		entry, err := store.Save("", doc)
		if err != nil || !regexp.MustCompile("^"+prefix+"-[a-f0-9]{32}$").MatchString(entry.ID) || !safeID.MatchString(entry.ID) {
			t.Fatalf("name %q: %#v %v", name, entry, err)
		}
		duplicate, err := store.Save("", doc)
		if err != nil || duplicate.ID == entry.ID {
			t.Fatalf("duplicate name: %#v %v", duplicate, err)
		}
		doc.Name = "Renamed"
		updated, err := store.Save(entry.ID, doc)
		if err != nil || updated.ID != entry.ID {
			t.Fatalf("update must keep ID stable: %#v %v", updated, err)
		}
	}
}

func TestDelete(t *testing.T) {
	store := New(t.TempDir())
	entry, err := store.Save("", sample())
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.Save("", sample())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(entry.ID); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("deleted entry: %v", err)
	}
	if _, err := store.Get(other.ID); err != nil {
		t.Fatalf("other entry: %v", err)
	}
	for _, id := range []string{entry.ID, "../outside", "/tmp/file", "a/b", "..", ""} {
		if err := store.Delete(id); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("missing/unsafe ID %q: %v", id, err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, marshal(t, sample()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(store.Directory, "configs", "link.json")); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("link"); err == nil {
		t.Fatal("accepted symlink")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("removed symlink target")
	}
	if err := os.WriteFile(filepath.Join(store.Directory, "configs", "legacy.json"), marshal(t, sample()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("legacy"); err != nil {
		t.Fatalf("legacy filename: %v", err)
	}
}

func TestColumnWidths(t *testing.T) {
	for _, width := range []float64{144, 280.5, 1024} {
		doc := sample()
		doc.Columns[0].Width = &width
		store := New(t.TempDir())
		entry, err := store.Save("", doc)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := store.Get(entry.ID)
		if err != nil || !reflect.DeepEqual(restored.Document, doc) {
			t.Fatalf("width round trip: %#v %v", restored, err)
		}
	}
	for _, width := range []float64{-1, 0, 143, math.Inf(1), math.NaN()} {
		doc := sample()
		doc.Columns[0].Width = &width
		if err := Validate(doc); err == nil {
			t.Fatalf("accepted width %v", width)
		}
	}
	for _, width := range []string{`null`, `"200"`, `false`, `0`, `143`, `1e999`} {
		data := strings.Replace(string(marshal(t, sample())), `"path":"timestamp"`, `"path":"timestamp","width":`+width, 1)
		if _, err := Decode([]byte(data)); err == nil {
			t.Fatalf("accepted width %s", width)
		}
	}
}
