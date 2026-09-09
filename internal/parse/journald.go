package parse

import (
	"encoding/json"
	"strconv"
	"time"

	"streamline/internal/logmodel"
)

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
		record.MessageIsJSON = true
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
