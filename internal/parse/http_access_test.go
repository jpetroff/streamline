package parse

import (
	"encoding/json"
	"strings"
	"testing"

	"streamline/internal/logmodel"
)

func TestHTTPAccessLayouts(t *testing.T) {
	cases := []struct {
		name, input, timestamp, message string
		fields                          map[string]any
	}{
		{
			name:      "common IPv4",
			input:     `192.0.2.10 - alice [10/Sep/2026:08:15:30 +0200] "GET /cloud?x=1 HTTP/1.1" 503 123`,
			timestamp: "2026-09-10T06:15:30Z", message: "GET /cloud?x=1 HTTP/1.1",
			fields: map[string]any{"client": "192.0.2.10", "ident": nil, "user": "alice", "status": json.Number("503"), "bytes": json.Number("123"), "method": "GET", "target": "/cloud?x=1", "protocol": "HTTP/1.1"},
		},
		{
			name:      "combined IPv6 and quoted escapes",
			input:     `2001:db8::1 - - [10/Sep/2026:08:15:30 -0430] "POST /api HTTP/2.0" 201 9007199254740993 "https://example.test/" "agent \"quoted\" \\path \xE4\xB8\x96\xE7\x95\x8C"`,
			timestamp: "2026-09-10T12:45:30Z", message: "POST /api HTTP/2.0",
			fields: map[string]any{"client": "2001:db8::1", "status": json.Number("201"), "bytes": json.Number("9007199254740993"), "referer": "https://example.test/", "user_agent": `agent "quoted" \path 世界`},
		},
		{
			name:      "missing values and failed request",
			input:     `- - - [10/Sep/2026:08:15:30 +0000] "-" - - "-" "-"`,
			timestamp: "2026-09-10T08:15:30Z", message: "-",
			fields: map[string]any{"client": nil, "ident": nil, "user": nil, "status": nil, "bytes": nil, "referer": nil, "user_agent": nil},
		},
		{
			name:      "malformed client request is a log",
			input:     `host.example ident user [10/Sep/2026:08:15:30 +0000] "bad request" 400 0`,
			timestamp: "2026-09-10T08:15:30Z", message: "bad request",
			fields: map[string]any{"client": "host.example", "ident": "ident", "user": "user", "status": json.Number("400"), "bytes": json.Number("0")},
		},
		{
			name:      "escaped controls are sanitized after decoding",
			input:     `192.0.2.1 - - [10/Sep/2026:08:15:30 +0000] "GET /%20 HTTP/1.1" 200 00042 "-" "\x1b[31mred\x1b[0m\x00\xff\nnext"`,
			timestamp: "2026-09-10T08:15:30Z", message: "GET /%20 HTTP/1.1",
			fields: map[string]any{"target": "/%20", "bytes": json.Number("42"), "user_agent": "red�\nnext"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := selectParser(tc.input, NewEngine(Options{}).context(), nil)
			r := d.Record
			if !d.IdentifiesLogs || r.SourceFormat != logmodel.FormatHTTPAccess || r.Timestamp != tc.timestamp || r.Message != tc.message || r.Severity != "" || r.MessageIsJSON {
				t.Fatalf("decision = %#v", d)
			}
			for key, value := range tc.fields {
				got, exists := r.Fields[key]
				if !exists || got != value {
					t.Errorf("%s = %#v, want %#v", key, got, value)
				}
			}
			if r.Fields["request"] != r.Message {
				t.Fatalf("request was not preserved: %#v", r)
			}
			if _, exists := tc.fields["method"]; !exists && (r.Message == "-" || r.Message == "bad request") {
				if _, exists := r.Fields["method"]; exists {
					t.Fatal("invented request components")
				}
			}
			if tc.name == "escaped controls are sanitized after decoding" {
				if !hasDiagnostic(r.Diagnostics, "terminal_controls_removed") || !hasDiagnostic(r.Diagnostics, "invalid_utf8_replaced") {
					t.Fatalf("missing cleanup diagnostics: %#v", r.Diagnostics)
				}
			} else if len(r.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %#v", r.Diagnostics)
			}
		})
	}
}

func TestHTTPAccessInvalidDatesRetainFields(t *testing.T) {
	for _, stamp := range []string{
		"30/Feb/2026:08:15:30 +0000", "10/Sep/2026:08:15:30 +2400",
		"10/Sep/2026:08:15:30 +0060", "10/Sep/2026:8:15:30 +0000",
	} {
		input := `192.0.2.1 - - [` + stamp + `] "GET / HTTP/1.1" 200 12`
		d := selectParser(input, NewEngine(Options{}).context(), nil)
		if !d.IdentifiesLogs || d.Record.Timestamp != "" || !hasDiagnostic(d.Record.Diagnostics, "invalid_timestamp") || d.Record.Fields["time"] != stamp {
			t.Fatalf("decision = %#v", d)
		}
	}
}

func TestMalformedHTTPAccessRetainsWholeLine(t *testing.T) {
	prefix := "192.0.2.1 - - [10/Sep/2026:08:15:30 +0000] "
	for _, suffix := range []string{
		`"GET / HTTP/1.1`, `"GET / HTTP/1.1"200 12`,
		`"GET / HTTP/1.1" 20 12`, `"GET / HTTP/1.1" 600 12`,
		`"GET / HTTP/1.1" 200 -1`, `"GET / HTTP/1.1" 200 1.5`,
		`"GET / HTTP/1.1" 200 18446744073709551616`,
		`"GET / HTTP/1.1" 200 12 "-" `,
		`"GET / HTTP/1.1" 200 12 "-""agent"`,
		`"GET / HTTP/1.1" 200 12 "-" "agent" 0.123`,
		`"GET / HTTP/1.1" 200 12 "-" "bad\q"`,
		`"GET / HTTP/1.1" 200 12 "-" "bad\xZZ"`,
		`"GET / HTTP/1.1" 200 12 "-" "bad\x1"`,
	} {
		input := prefix + suffix
		t.Run(suffix, func(t *testing.T) {
			d := selectParser(input, NewEngine(Options{}).context(), nil)
			if d.IdentifiesLogs || d.Record.SourceFormat != logmodel.FormatText || d.Record.Message != input || d.Record.Fields != nil || !hasDiagnostic(d.Record.Diagnostics, "malformed_http_access_fallback") {
				t.Fatalf("decision = %#v", d)
			}
			result, err := NewEngine(Options{}).Load(strings.NewReader(input))
			if err != nil || result.Kind != ResultRaw || result.Raw.Text != input {
				t.Fatalf("load = %#v, %v", result, err)
			}
		})
	}
}

func TestHTTPAccessDoesNotGuessCustomLayouts(t *testing.T) {
	for _, input := range []string{
		`vhost 192.0.2.1 - - [10/Sep/2026:08:15:30 +0000] "GET / HTTP/1.1" 200 12`,
		`192.0.2.1 [10/Sep/2026:08:15:30 +0000] "GET / HTTP/1.1" 200 12`,
		`a sentence mentioning "GET / HTTP/1.1" 200 12`,
	} {
		d := selectParser(input, NewEngine(Options{}).context(), nil)
		if d.IdentifiesLogs || d.Record.SourceFormat != logmodel.FormatText || d.Record.Message != input {
			t.Fatalf("false recognition: %#v", d)
		}
	}
}

func TestHTTPAccessEscapedIdentityAndInvalidRequestTarget(t *testing.T) {
	input := `host.example - user\x20name [10/Sep/2026:12:00:00 +0000] "GET /bad\x00target HTTP/1.1" 400 12`
	d := selectParser(input, NewEngine(Options{}).context(), nil)
	if !d.IdentifiesLogs || d.Record.Fields["user"] != "user name" || d.Record.Fields["method"] != nil || d.Record.Message != "GET /badtarget HTTP/1.1" || !hasDiagnostic(d.Record.Diagnostics, "terminal_controls_removed") {
		t.Fatalf("escaped identity/request = %#v", d)
	}
}
