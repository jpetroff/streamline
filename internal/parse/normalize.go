package parse

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"streamline/internal/logmodel"
)

func cleanJSONObject(object map[string]any, diagnostics *[]logmodel.Diagnostic) map[string]any {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	cleaned := make(map[string]any, len(object))
	for _, key := range keys {
		cleanedKey := sanitizeBytes([]byte(key), false)
		noteSanitization(diagnostics, cleanedKey)
		if _, exists := cleaned[cleanedKey.text]; exists {
			addDiagnostic(diagnostics, "field_key_collision", "terminal cleanup caused multiple source fields to share a key")
		}
		cleaned[cleanedKey.text] = cleanJSONValue(object[key], diagnostics)
	}
	return cleaned
}

func cleanJSONValue(value any, diagnostics *[]logmodel.Diagnostic) any {
	switch value := value.(type) {
	case string:
		cleaned := sanitizeBytes([]byte(value), true)
		noteSanitization(diagnostics, cleaned)
		return cleaned.text
	case map[string]any:
		return cleanJSONObject(value, diagnostics)
	case []any:
		cleaned := make([]any, len(value))
		for index := range value {
			cleaned[index] = cleanJSONValue(value[index], diagnostics)
		}
		return cleaned
	default:
		return value
	}
}

// normalizeFields shares alias precedence between JSON and logfmt. Source values
// retain their types; only canonical message, severity, and timestamp are derived.
func normalizeFields(fields map[string]any, format logmodel.SourceFormat, context parseContext, diagnostics []logmodel.Diagnostic) logmodel.Record {
	record := logmodel.Record{
		Fields:       fields,
		SourceFormat: format,
		Diagnostics:  diagnostics,
	}

	messageFound := false
	for _, key := range []string{"message", "msg", "log"} {
		value, exists := fields[key]
		if !exists || value == nil {
			continue
		}
		if text, ok := value.(string); ok {
			record.Message = text
		} else {
			record.Message = compactJSON(value)
			record.MessageIsJSON = true
			addDiagnostic(&record.Diagnostics, "non_text_message", key+" was serialized because it was not a string")
		}
		messageFound = true
		break
	}
	if !messageFound {
		record.Message = compactJSON(fields)
		record.MessageIsJSON = true
		addDiagnostic(&record.Diagnostics, "missing_message", "structured record has no usable message, msg, or log field")
	}

	for _, key := range []string{"severity", "level", "lvl", "priority"} {
		value, exists := fields[key]
		if !exists || value == nil {
			continue
		}
		if text, ok := value.(string); ok {
			record.Severity = normalizeSeverity(text)
		} else if key == "priority" {
			if number, ok := value.(json.Number); ok {
				if priority, err := strconv.Atoi(number.String()); err == nil {
					record.Severity = syslogSeverity(priority)
				}
			}
		}
		if record.Severity == "" {
			addDiagnostic(&record.Diagnostics, "invalid_severity", key+" is not a recognized severity")
		}
		if record.Severity != "" {
			break
		}
	}

	for _, key := range []string{"timestamp", "time", "ts", "@timestamp"} {
		value, exists := fields[key]
		if !exists || value == nil {
			continue
		}
		var timestamp time.Time
		var assumed, ok bool
		switch value := value.(type) {
		case string:
			timestamp, assumed, ok = parseTimestampString(value, context)
		case json.Number:
			if key == "ts" {
				timestamp, ok = parseUnixSeconds(value.String())
			}
		}
		if ok {
			record.Timestamp = formatTimestamp(timestamp)
			if assumed {
				addDiagnostic(&record.Diagnostics, "timestamp_context_assumed", key+" omitted a timezone or year; configured context was applied")
			}
			break
		}
		addDiagnostic(&record.Diagnostics, "invalid_timestamp", key+" is not a recognized timestamp")
	}

	return record
}

func compactJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}
