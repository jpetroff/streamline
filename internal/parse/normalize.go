package parse

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"streamline/internal/logmodel"
)

func parseRecord(input string, context parseContext, diagnostics []logmodel.Diagnostic) logmodel.Record {
	trimmed := strings.TrimSpace(input)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		value, err := decodeJSON(trimmed)
		if err != nil {
			addDiagnostic(&diagnostics, "malformed_json_fallback", "JSON-like input could not be decoded and was retained as text")
			return normalizeText(input, context, diagnostics)
		}
		object, ok := value.(map[string]any)
		if !ok {
			addDiagnostic(&diagnostics, "unsupported_json_root", "only JSON objects are normalized as structured log records")
			return normalizeText(input, context, diagnostics)
		}
		object = cleanJSONObject(object, &diagnostics)
		if looksLikeJournald(object) {
			return normalizeJournald(object, diagnostics)
		}
		return normalizeJSON(object, context, diagnostics)
	}
	return normalizeText(input, context, diagnostics)
}

func decodeJSON(input string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

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

func noteSanitization(diagnostics *[]logmodel.Diagnostic, cleaned sanitizedText) {
	if cleaned.hadTerminal {
		addDiagnostic(diagnostics, "terminal_controls_removed", "terminal control sequences or unsafe control characters were removed")
	}
	if cleaned.invalidUTF8 {
		addDiagnostic(diagnostics, "invalid_utf8_replaced", "invalid UTF-8 bytes were replaced")
	}
}

func looksLikeJournald(object map[string]any) bool {
	if _, ok := object["__REALTIME_TIMESTAMP"]; !ok {
		return false
	}
	for _, key := range []string{"__CURSOR", "__MONOTONIC_TIMESTAMP", "_BOOT_ID"} {
		if _, ok := object[key]; ok {
			return true
		}
	}
	return false
}

func normalizeJournald(fields map[string]any, diagnostics []logmodel.Diagnostic) logmodel.Record {
	record := logmodel.Record{
		Fields:       fields,
		SourceFormat: logmodel.FormatJournaldJSON,
		Diagnostics:  diagnostics,
	}

	messageFound := false
	if value, exists := fields["MESSAGE"]; exists {
		record.Message, messageFound = journalMessage(value, &record.Diagnostics)
		if !messageFound {
			addDiagnostic(&record.Diagnostics, "non_text_message", "journald MESSAGE was not a usable text or byte-array value")
		}
	}
	if !messageFound {
		if _, exists := fields["MESSAGE"]; !exists {
			addDiagnostic(&record.Diagnostics, "missing_message", "journald record does not contain MESSAGE")
		}
		record.Message = compactJSON(fields)
	}

	for _, key := range []string{"_SOURCE_REALTIME_TIMESTAMP", "__REALTIME_TIMESTAMP"} {
		value, exists := fields[key]
		if !exists || value == nil {
			continue
		}
		text, repeated, ok := firstJournalString(value)
		if repeated {
			addDiagnostic(&record.Diagnostics, "multiple_journal_values", key+" contains multiple values; the first usable value was selected")
		}
		if ok {
			if timestamp, valid := parseJournalMicroseconds(text); valid {
				record.Timestamp = formatTimestamp(timestamp)
				break
			}
		}
		addDiagnostic(&record.Diagnostics, "invalid_timestamp", key+" is not a valid journal microsecond timestamp")
	}

	if value, exists := fields["PRIORITY"]; exists {
		text, repeated, ok := firstJournalString(value)
		if repeated {
			addDiagnostic(&record.Diagnostics, "multiple_journal_values", "PRIORITY contains multiple values; the first usable value was selected")
		}
		if ok {
			priority, err := strconv.Atoi(text)
			if err == nil {
				record.Severity = syslogSeverity(priority)
			}
		}
		if record.Severity == "" {
			addDiagnostic(&record.Diagnostics, "invalid_severity", "PRIORITY is not a recognized syslog priority")
		}
	}

	return record
}

func journalMessage(value any, diagnostics *[]logmodel.Diagnostic) (string, bool) {
	if binary, ok := journalByteArray(value); ok {
		cleaned := sanitizeBytes(binary, true)
		noteSanitization(diagnostics, cleaned)
		return cleaned.text, true
	}
	if values, ok := value.([]any); ok {
		if len(values) > 1 {
			addDiagnostic(diagnostics, "multiple_journal_values", "MESSAGE contains multiple values; the first usable value was selected")
		}
		for _, candidate := range values {
			if text, ok := journalMessage(candidate, diagnostics); ok {
				return text, true
			}
		}
		return "", false
	}
	if text, ok := value.(string); ok {
		return text, true
	}
	return "", false
}

func journalByteArray(value any) ([]byte, bool) {
	values, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]byte, len(values))
	for index, value := range values {
		number, ok := value.(json.Number)
		if !ok {
			return nil, false
		}
		integer, err := number.Int64()
		if err != nil || integer < 0 || integer > 255 {
			return nil, false
		}
		result[index] = byte(integer)
	}
	return result, true
}

func firstJournalString(value any) (string, bool, bool) {
	switch value := value.(type) {
	case string:
		return value, false, true
	case json.Number:
		return value.String(), false, true
	case []any:
		for _, candidate := range value {
			if text, _, ok := firstJournalString(candidate); ok {
				return text, len(value) > 1, true
			}
		}
		return "", len(value) > 1, false
	default:
		return "", false, false
	}
}

func parseJournalMicroseconds(input string) (time.Time, bool) {
	microseconds, err := strconv.ParseInt(input, 10, 64)
	if err != nil || microseconds < 0 {
		return time.Time{}, false
	}
	value := time.Unix(microseconds/1_000_000, (microseconds%1_000_000)*1_000)
	if value.Year() < 0 || value.Year() > 9999 {
		return time.Time{}, false
	}
	return value, true
}

func normalizeJSON(fields map[string]any, context parseContext, diagnostics []logmodel.Diagnostic) logmodel.Record {
	record := logmodel.Record{
		Fields:       fields,
		SourceFormat: logmodel.FormatJSON,
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
			addDiagnostic(&record.Diagnostics, "non_text_message", key+" was serialized because it was not a string")
		}
		messageFound = true
		break
	}
	if !messageFound {
		record.Message = compactJSON(fields)
		addDiagnostic(&record.Diagnostics, "missing_message", "JSON object has no usable message, msg, or log field")
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

func normalizeText(input string, context parseContext, diagnostics []logmodel.Diagnostic) logmodel.Record {
	record := logmodel.Record{
		Message:      input,
		SourceFormat: logmodel.FormatText,
		Diagnostics:  diagnostics,
	}
	withoutIndent := strings.TrimLeft(input, " \t")
	if timestamp, remainder, assumed, ok := parseLeadingTimestamp(withoutIndent, context); ok {
		record.Timestamp = formatTimestamp(timestamp)
		record.Message = strings.TrimLeft(remainder, " \t")
		if assumed {
			addDiagnostic(&record.Diagnostics, "timestamp_context_assumed", "plain-text timestamp omitted a timezone or year; configured context was applied")
		}
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

func normalizeSeverity(input string) string {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "emerg", "emergency", "alert", "crit", "critical", "fatal", "panic", "dpanic":
		return "fatal"
	case "err", "error":
		return "error"
	case "warn", "warning":
		return "warn"
	case "notice", "info", "information":
		return "info"
	case "debug", "trace":
		return "debug"
	default:
		return ""
	}
}

func syslogSeverity(priority int) string {
	switch {
	case priority >= 0 && priority <= 2:
		return "fatal"
	case priority == 3:
		return "error"
	case priority == 4:
		return "warn"
	case priority == 5 || priority == 6:
		return "info"
	case priority == 7:
		return "debug"
	default:
		return ""
	}
}

func addDiagnostic(diagnostics *[]logmodel.Diagnostic, code, message string) {
	for _, existing := range *diagnostics {
		if existing.Code == code {
			return
		}
	}
	*diagnostics = append(*diagnostics, logmodel.Diagnostic{Code: code, Message: message})
}

func hasDiagnostic(diagnostics []logmodel.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
