package parse

import (
	"strings"
	"testing"

	"streamline/internal/logmodel"
)

func TestSelectorPrecedenceAndMixedFormats(t *testing.T) {
	cases := []struct {
		input      string
		format     logmodel.SourceFormat
		recognized bool
		diagnostic string
	}{
		{`{"MESSAGE":"msg=embedded","__REALTIME_TIMESTAMP":"0","__CURSOR":"cursor","msg":"generic"}`, logmodel.FormatJournaldJSON, true, ""},
		{`{"msg":"level=info msg=embedded"}`, logmodel.FormatJSON, true, ""},
		{`{"msg":broken} key=value`, logmodel.FormatText, false, "malformed_json_fallback"},
		{`["msg=embedded"]`, logmodel.FormatText, false, "unsupported_json_root"},
		{`status=403`, logmodel.FormatLogfmt, true, "missing_message"},
		{`2026-01-01T00:00:00Z msg=keep-as-text`, logmodel.FormatText, true, ""},
		{`prose mentioning status=403`, logmodel.FormatText, false, ""},
		{`plain prose`, logmodel.FormatText, false, ""},
		{`msg="broken`, logmodel.FormatText, false, "malformed_logfmt_fallback"},
	}
	var lines []string
	for _, tc := range cases {
		d := selectParser(tc.input, NewEngine(Options{}).context(), nil)
		if d.Record.SourceFormat != tc.format || d.IdentifiesLogs != tc.recognized {
			t.Errorf("%s: %#v", tc.input, d)
		}
		if tc.diagnostic != "" && !hasDiagnostic(d.Record.Diagnostics, tc.diagnostic) {
			t.Errorf("%s: missing %s", tc.input, tc.diagnostic)
		}
		lines = append(lines, tc.input)
	}
	result, err := NewEngine(Options{}).Load(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ResultParsed || len(result.Parsed.Records) != len(cases) {
		t.Fatal("mixed source did not preserve records")
	}
	for i, tc := range cases {
		if result.Parsed.Records[i].Entry.SourceFormat != tc.format {
			t.Fatalf("mixed record %d parser changed", i)
		}
	}
	if result.Parsed.Records[0].Entry.Message != "msg=embedded" || result.Parsed.Records[1].Entry.Message != "level=info msg=embedded" {
		t.Fatal("embedded message was reinterpreted")
	}
}

func TestTextModeBypassesAllParsers(t *testing.T) {
	input := "\x1b[31m" + `msg=one msg=two` + "\x1b[0m\n" + `msg="broken` + "\n" + `{"message":"json"}` + "\n2026-01-01T00:00:00Z timestamp"
	result, err := NewEngine(Options{Text: true}).Load(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`msg=one msg=two`, `msg="broken`, `{"message":"json"}`, "2026-01-01T00:00:00Z timestamp"}
	if result.Kind != ResultParsed || len(result.Parsed.Records) != len(want) {
		t.Fatal("text record count")
	}
	for i, captured := range result.Parsed.Records {
		r := captured.Entry
		if r.Message != want[i] || r.SourceFormat != logmodel.FormatText || r.Fields != nil || r.Timestamp != "" || r.Severity != "" {
			t.Fatalf("text record %d = %#v", i, r)
		}
		for _, d := range r.Diagnostics {
			if d.Code != "terminal_controls_removed" {
				t.Errorf("parser diagnostic leaked into Text mode: %s", d.Code)
			}
		}
	}
}
