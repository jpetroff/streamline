// Package logmodel defines the normalized record shared by parsers and queries.
package logmodel

import "encoding/json"

// SourceFormat identifies the parser that recognized a record.
type SourceFormat string

const (
	FormatJournaldJSON  SourceFormat = "journald-json"
	FormatJSON          SourceFormat = "json"
	FormatLogfmt        SourceFormat = "logfmt"
	FormatSyslogRFC5424 SourceFormat = "syslog-rfc5424"
	FormatSyslogRFC3164 SourceFormat = "syslog-rfc3164"
	FormatSyslogText    SourceFormat = "syslog-text"
	FormatHTTPAccess    SourceFormat = "http-access"
	FormatText          SourceFormat = "text"
)

// Diagnostic describes a non-fatal normalization or data-quality issue.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Record is the canonical in-memory log representation. Fields must contain
// only values supported by JSON: nil, booleans, strings, json.Number values,
// []any values, and map[string]any values.
type Record struct {
	ID        uint64 `json:"-"`
	Timestamp string `json:"timestamp,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Message   string `json:"message"`
	// MessageIsJSON marks a display fallback serialized from structured values.
	// Search must visit Fields instead, otherwise object keys become matches.
	MessageIsJSON bool           `json:"-"`
	Fields        map[string]any `json:"fields,omitempty"`
	SourceFormat  SourceFormat   `json:"sourceFormat"`
	Diagnostics   []Diagnostic   `json:"diagnostics,omitempty"`
}

// CloneRecord copies all mutable JSON trees owned by a record.
func CloneRecord(record Record) Record {
	record.Fields = CloneFields(record.Fields)
	record.Diagnostics = append([]Diagnostic(nil), record.Diagnostics...)
	return record
}

// CloneFields returns a deep copy of a JSON-compatible object.
func CloneFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	cloned := make(map[string]any, len(fields))
	for key, value := range fields {
		cloned[key] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return CloneFields(value)
	case []any:
		cloned := make([]any, len(value))
		for i := range value {
			cloned[i] = cloneJSONValue(value[i])
		}
		return cloned
	case json.Number, string, bool, nil:
		return value
	default:
		// Parser output is constrained to the cases above. Keeping an unknown
		// scalar intact is safer for manually constructed query records than
		// silently dropping it.
		return value
	}
}
