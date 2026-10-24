package parse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"streamline/internal/logmodel"
)

var errUnsupportedJSONDocument = errors.New("expected one JSON object or an array containing only objects")

// streamJSONDocument deliberately waits for clean EOF. Neither an object boundary
// nor the closing array delimiter proves that no invalid suffix will follow.
func (e *Engine) streamJSONDocument(reader io.Reader, emit func([]CapturedRecord)) (*LoadResult, error) {
	context := e.context()
	source, readErr := io.ReadAll(reader)
	result := &LoadResult{Source: source}
	raw := func() {
		result.Kind = ResultRaw
		result.Raw = &RawResult{Text: sanitizeBytes(source, true).text}
	}
	if readErr != nil {
		raw()
		result.Diagnostics = append(result.Diagnostics, LoadDiagnostic{
			Diagnostic: logmodel.Diagnostic{Code: "input_read_error", Message: readErr.Error()},
			RawStart:   len(source), RawEnd: len(source),
		})
		return result, fmt.Errorf("read log input: %w", readErr)
	}

	records, err := decodeJSONDocument(source, context)
	if err != nil {
		raw()
		code := "malformed_json_document"
		if errors.Is(err, errUnsupportedJSONDocument) {
			code = "unsupported_json_document"
		}
		result.Diagnostics = append(result.Diagnostics, LoadDiagnostic{
			Diagnostic: logmodel.Diagnostic{Code: code, Message: err.Error()},
			RawStart:   0, RawEnd: len(source),
		})
		return result, nil
	}
	result.Kind = ResultParsed
	result.Parsed = &ParsedResult{Records: records}
	if emit != nil && len(records) > 0 {
		emit(append([]CapturedRecord(nil), records...))
	}
	return result, nil
}

// decodeJSONDocument uses RawMessage to locate original object spans, independent
// of any cleanup that changes byte lengths. No records escape on any failure.
func decodeJSONDocument(source []byte, context parseContext) ([]CapturedRecord, error) {
	decoder := json.NewDecoder(bytes.NewReader(source))
	records := make([]CapturedRecord, 0)
	unsupported := false
	decodeRecord := func() error {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return err
		}
		end := int(decoder.InputOffset())
		start := end - len(raw)
		if len(raw) == 0 || raw[0] != '{' {
			unsupported = true
			return nil
		}
		value, err := decodeJSON(string(raw))
		if err != nil {
			return err
		}
		diagnostics := []logmodel.Diagnostic{}
		if !utf8.Valid(raw) {
			addDiagnostic(&diagnostics, "invalid_utf8_replaced", "invalid UTF-8 bytes were replaced")
		}
		object := cleanJSONObject(value.(map[string]any), &diagnostics)
		record := normalizeJSONObject(object, context, diagnostics)
		records = append(records, CapturedRecord{Entry: record, RawStart: start, RawEnd: end})
		return nil
	}

	trimmed := bytes.TrimSpace(source)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		for decoder.More() {
			if err := decodeRecord(); err != nil {
				return nil, err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
	} else if err := decodeRecord(); err != nil {
		return nil, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("expected exactly one JSON document")
		}
		return nil, err
	}
	if unsupported {
		return nil, errUnsupportedJSONDocument
	}
	return records, nil
}
