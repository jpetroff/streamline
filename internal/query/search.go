package query

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// SearchSpec is an independent, last-stage filter over all log values.
// Text retains physical lines so compilation failures map back to the editor.
type SearchSpec struct {
	Text     string `json:"text"`
	Mode     string `json:"mode"`
	Operator string `json:"operator"`
}

// LineError describes one invalid expression at its original one-based line.
type LineError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type searchLine struct {
	number int
	text   string
}

// searchLines normalizes separators, ignoring blank lines without trimming
// meaningful whitespace or renumbering the remaining expressions.
func searchLines(text string) []searchLine {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var lines []searchLine
	for i, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, searchLine{number: i + 1, text: line})
		}
	}
	return lines
}

// compileSearch validates the complete specification before returning an
// immutable predicate. Compilation happens once per query, never per record.
// The browser checks JS syntax; Go compilation remains authoritative here.
func compileSearch(spec *SearchSpec) (Predicate, error) {
	if spec == nil {
		return allPredicates(), nil
	}
	if spec.Mode != "" && spec.Mode != "plain" && spec.Mode != "regexp" {
		return nil, &APIError{Code: "invalid_search", Message: "search mode must be plain or regexp"}
	}
	if spec.Operator != "" && spec.Operator != "or" && spec.Operator != "and" {
		return nil, &APIError{Code: "invalid_search", Message: "search operator must be or or and"}
	}
	var predicates []Predicate
	var issues []LineError
	for _, line := range searchLines(spec.Text) {
		matcher, err := compileLine(line.text, spec.Mode)
		if err != nil {
			issues = append(issues, LineError{Line: line.number, Message: err.Error()})
			continue
		}
		predicates = append(predicates, func(record Record) bool {
			return anyRecordValue(record, matcher.MatchString)
		})
	}
	if len(issues) > 0 {
		return nil, &APIError{Code: "invalid_search", Message: "Some search expressions are not supported; check the marked lines.", LineErrors: issues}
	}
	// An empty search is the identity filter for either operator. AND combines
	// whole-record predicates: different lines may match different fields.
	if len(predicates) == 0 || spec.Operator == "and" {
		return allPredicates(predicates...), nil
	}
	return anyPredicates(predicates...), nil
}

// compileLine preserves literal substring semantics in plain mode and uses
// the same Unicode case folding for both modes. Explicit regexp flags can
// override the default within an expression; no syntax translation is done.
func compileLine(text, mode string) (*regexp.Regexp, error) {
	if mode != "regexp" {
		text = regexp.QuoteMeta(text)
	}
	return regexp.Compile("(?i)" + text)
}

// allPredicates composes ordered filters with short-circuit evaluation. The
// copied list prevents callers from changing a predicate after query creation.
func allPredicates(predicates ...Predicate) Predicate {
	filters := append([]Predicate(nil), predicates...)
	return func(record Record) bool {
		for _, predicate := range filters {
			if !predicate(record) {
				return false
			}
		}
		return true
	}
}

// anyPredicates combines whole-record results without carrying state between entries.
func anyPredicates(predicates ...Predicate) Predicate {
	filters := append([]Predicate(nil), predicates...)
	return func(record Record) bool {
		for _, predicate := range filters {
			if predicate(record) {
				return true
			}
		}
		return false
	}
}

// anyRecordValue explicitly selects searchable data, excluding transport and
// parser metadata. Synthetic JSON messages would leak keys, so their original
// values are visited through Fields instead. Genuine JSON-looking strings stay.
func anyRecordValue(record Record, match func(string) bool) bool {
	return (record.Timestamp != "" && match(record.Timestamp)) ||
		(record.Severity != "" && match(record.Severity)) ||
		(!record.MessageIsJSON && match(record.Message)) ||
		anyScalar(record.Fields, match)
}

// anyScalar visits decoded JSON leaves independently. Containers, keys, and
// paths never become candidates, and matches cannot span two separate values.
// It has no side effects and stops as soon as a scalar satisfies the matcher.
func anyScalar(value any, match func(string) bool) bool {
	switch value := value.(type) {
	case map[string]any:
		for _, child := range value {
			if anyScalar(child, match) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if anyScalar(child, match) {
				return true
			}
		}
	case string:
		return match(value)
	case json.Number:
		return match(value.String())
	case bool:
		return match(strconv.FormatBool(value))
	case nil:
		return match("null")
	}
	return false
}

// ValidateSearch checks query search syntax without allocating a query or scanning logs.
func ValidateSearch(spec *SearchSpec) error {
	_, err := compileSearch(spec)
	return err
}
