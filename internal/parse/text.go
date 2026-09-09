package parse

import (
	"strings"

	"streamline/internal/logmodel"
)

func normalizeText(input string, context parseContext, diagnostics []logmodel.Diagnostic) logmodel.Record {
	record := plainText(input, diagnostics)
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

// plainText preserves a sanitized line without attempting timestamp detection.
func plainText(input string, diagnostics []logmodel.Diagnostic) logmodel.Record {
	return logmodel.Record{Message: input, SourceFormat: logmodel.FormatText, Diagnostics: diagnostics}
}
