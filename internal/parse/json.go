package parse

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"streamline/internal/logmodel"
)

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

// normalizeJSON applies the shared structured-field aliases to JSON values.
func normalizeJSON(fields map[string]any, context parseContext, diagnostics []logmodel.Diagnostic) logmodel.Record {
	return normalizeFields(fields, logmodel.FormatJSON, context, diagnostics)
}
