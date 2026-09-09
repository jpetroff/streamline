package parse

import "streamline/internal/logmodel"

func noteSanitization(diagnostics *[]logmodel.Diagnostic, cleaned sanitizedText) {
	if cleaned.hadTerminal {
		addDiagnostic(diagnostics, "terminal_controls_removed", "terminal control sequences or unsafe control characters were removed")
	}
	if cleaned.invalidUTF8 {
		addDiagnostic(diagnostics, "invalid_utf8_replaced", "invalid UTF-8 bytes were replaced")
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
