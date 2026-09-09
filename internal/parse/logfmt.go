package parse

import (
	"fmt"
	"strconv"
	"strings"

	"streamline/internal/logmodel"
)

func looksLikeLogfmt(input string) bool {
	firstToken, _, _ := strings.Cut(input, " ")
	firstToken, _, _ = strings.Cut(firstToken, "\t")
	return strings.Contains(firstToken, "=")
}

// scanLogfmt consumes a whole line of space/tab-separated key=value pairs.
// Keys are nonempty and unquoted. Values are literal unquoted strings or Go
// double-quoted strings, as emitted by Logrus. No scalar types are inferred.
// A failed scan returns no fields or partial normalization diagnostics.
func scanLogfmt(input string) (map[string]any, []logmodel.Diagnostic, error) {
	fields := make(map[string]any)
	var diagnostics []logmodel.Diagnostic
	fail := func(offset int, reason string) (map[string]any, []logmodel.Diagnostic, error) {
		return nil, nil, fmt.Errorf("byte %d: %s", offset+1, reason)
	}
	for pos := 0; pos < len(input); {
		if isLogfmtSpace(input[pos]) {
			pos++
			continue
		}
		start := pos
		for pos < len(input) && input[pos] != '=' && input[pos] != '"' && !isLogfmtSpace(input[pos]) {
			pos++
		}
		if pos == start || pos == len(input) || input[pos] != '=' {
			return fail(pos, "expected a nonempty unquoted key followed by =")
		}
		key := input[start:pos]
		pos++
		start = pos
		var value string
		if pos < len(input) && input[pos] == '"' {
			pos++
			for pos < len(input) && input[pos] != '"' {
				if input[pos] == '\\' {
					pos++
				}
				pos++
			}
			if pos >= len(input) {
				return fail(start, "unterminated quoted value")
			}
			pos++
			decoded, err := strconv.Unquote(input[start:pos])
			if err != nil {
				return fail(start, "invalid quoted value or escape")
			}
			value = decoded
			if pos < len(input) && !isLogfmtSpace(input[pos]) {
				return fail(pos, "expected whitespace after quoted value")
			}
		} else {
			for pos < len(input) && !isLogfmtSpace(input[pos]) {
				if input[pos] == '"' {
					return fail(pos, "unexpected quote in unquoted value")
				}
				pos++
			}
			value = input[start:pos]
		}
		cleaned := sanitizeBytes([]byte(value), true)
		noteSanitization(&diagnostics, cleaned)
		if _, exists := fields[key]; exists {
			addDiagnostic(&diagnostics, "duplicate_logfmt_key", "duplicate key/value fields use the last value")
		}
		fields[key] = cleaned.text
	}
	if len(fields) == 0 {
		return fail(0, "expected at least one key=value pair")
	}
	return fields, diagnostics, nil
}

func isLogfmtSpace(character byte) bool {
	return character == ' ' || character == '\t'
}

func normalizeLogfmt(fields map[string]any, context parseContext, diagnostics []logmodel.Diagnostic) logmodel.Record {
	return normalizeFields(fields, logmodel.FormatLogfmt, context, diagnostics)
}
