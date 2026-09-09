package parse

import (
	"strings"

	"streamline/internal/logmodel"
)

// parseDecision separates record provenance (Record.SourceFormat) from the
// evidence that lets Engine commit a pending source to parsed output.
type parseDecision struct {
	Record         logmodel.Record
	IdentifiesLogs bool
}

// selectParser is the single, ordered choice mechanism for Auto mode. Selection
// runs per line, so a source can mix formats. Text mode bypasses this function.
// Add formats here, before the text fallback, with explicit recognition rules.
func selectParser(input string, context parseContext, diagnostics []logmodel.Diagnostic) parseDecision {
	trimmed := strings.TrimSpace(input)

	// 1. JSON-shaped input belongs exclusively to JSON, even if malformed.
	// Decode once; journald's identifying fields take precedence over aliases.
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		value, err := decodeJSON(trimmed)
		if err != nil {
			addDiagnostic(&diagnostics, "malformed_json_fallback", "JSON-like input could not be decoded and was retained as text")
			return parseDecision{Record: normalizeText(input, context, diagnostics)}
		}
		object, ok := value.(map[string]any)
		if !ok {
			addDiagnostic(&diagnostics, "unsupported_json_root", "only JSON objects are normalized as structured log records")
			return parseDecision{Record: normalizeText(input, context, diagnostics)}
		}
		object = cleanJSONObject(object, &diagnostics)
		if looksLikeJournald(object) {
			return parseDecision{Record: normalizeJournald(object, diagnostics), IdentifiesLogs: true}
		}
		return parseDecision{Record: normalizeJSON(object, context, diagnostics), IdentifiesLogs: true}
	}

	// 2. A leading key=value token is a logfmt candidate. Only a complete
	// successful scan identifies logs; malformed candidates keep the whole line.
	logfmtInput := strings.Trim(input, " \t")
	if looksLikeLogfmt(logfmtInput) {
		fields, issues, err := scanLogfmt(logfmtInput)
		if err != nil {
			addDiagnostic(&diagnostics, "malformed_logfmt_fallback", "key/value input was retained as text: "+err.Error())
			return parseDecision{Record: plainText(input, diagnostics)}
		}
		for _, issue := range issues {
			addDiagnostic(&diagnostics, issue.Code, issue.Message)
		}
		return parseDecision{Record: normalizeLogfmt(fields, context, diagnostics), IdentifiesLogs: true}
	}

	// 3. Timestamped text recognizes logs; 4. other text remains a fallback.
	record := normalizeText(input, context, diagnostics)
	return parseDecision{Record: record, IdentifiesLogs: record.Timestamp != ""}
}
