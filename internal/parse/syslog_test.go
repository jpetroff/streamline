package parse

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"streamline/internal/logmodel"
)

func TestSyslogHeadersAndNormalization(t *testing.T) {
	context := NewEngine(Options{
		DefaultLocation: time.FixedZone("home", 3*60*60),
		ReferenceTime:   time.Date(2027, 1, 1, 0, 0, 30, 0, time.FixedZone("home", 3*60*60)),
	}).context()
	cases := []struct {
		name, input, timestamp, message, severity string
		format                                    logmodel.SourceFormat
		fields                                    map[string]any
		diagnostics                               []string
	}{
		{
			name:      "RFC5424 complete",
			input:     `<165>1 2026-09-10T08:15:30.123456+02:00 nas backup worker/1 JOB42 [meta sequenceId="7"][origin ip="192.0.2.1" ip="2001:db8::1"] backup complete`,
			timestamp: "2026-09-10T06:15:30.123456Z", message: "backup complete", severity: "info", format: logmodel.FormatSyslogRFC5424,
			fields: map[string]any{
				"version": json.Number("1"), "time": "2026-09-10T08:15:30.123456+02:00", "hostname": "nas", "app": "backup", "procid": "worker/1", "msgid": "JOB42",
				"priority": json.Number("165"), "facility": json.Number("20"), "severity_code": json.Number("5"),
				"structured_data": map[string]any{"meta": map[string]any{"sequenceId": "7"}, "origin": map[string]any{"ip": []any{"192.0.2.1", "2001:db8::1"}}},
			},
		},
		{
			name: "RFC5424 nil values without message", input: "<0>1 - - - - - -",
			format: logmodel.FormatSyslogRFC5424, severity: "fatal",
			fields: map[string]any{"version": json.Number("1"), "time": nil, "hostname": nil, "app": nil, "procid": nil, "msgid": nil, "structured_data": nil, "priority": json.Number("0"), "facility": json.Number("0"), "severity_code": json.Number("0")},
		},
		{
			name:   "RFC5424 escapes and UTF8 BOM",
			input:  `<191>1 - host app - - [example@32473 value="quote:\" slash:\\ bracket:\] unknown:\q" empty=""] ` + "\ufeff" + "  世界  ",
			format: logmodel.FormatSyslogRFC5424, severity: "debug", message: "  世界  ",
			fields: map[string]any{"version": json.Number("1"), "time": nil, "hostname": "host", "app": "app", "procid": nil, "msgid": nil,
				"priority": json.Number("191"), "facility": json.Number("23"), "severity_code": json.Number("7"),
				"structured_data": map[string]any{"example@32473": map[string]any{"value": `quote:" slash:\ bracket:] unknown:\q`, "empty": ""}}},
		},
		{
			name: "RFC3164 year rollover", input: "<34>Dec 31 23:59:59 nas sshd[0042]: Failed password",
			format: logmodel.FormatSyslogRFC3164, severity: "fatal", timestamp: "2026-12-31T20:59:59Z", message: "Failed password",
			fields:      map[string]any{"time": "Dec 31 23:59:59", "hostname": "nas", "app": "sshd", "procid": "0042", "priority": json.Number("34"), "facility": json.Number("4"), "severity_code": json.Number("2")},
			diagnostics: []string{"timestamp_context_assumed"},
		},
		{
			name: "local syslog has no priority", input: "Jan  1 00:00:00 home systemd-resolved[7]: ready",
			format: logmodel.FormatSyslogText, timestamp: "2026-12-31T21:00:00Z", message: "ready",
			fields:      map[string]any{"time": "Jan  1 00:00:00", "hostname": "home", "app": "systemd-resolved", "procid": "7"},
			diagnostics: []string{"timestamp_context_assumed"},
		},
		{
			name: "local ISO timestamp", input: "2026-09-10T08:15:30.123456789-04:00 2001:db8::1 kernel:  message  ",
			format: logmodel.FormatSyslogText, timestamp: "2026-09-10T12:15:30.123456789Z", message: " message  ",
			fields: map[string]any{"time": "2026-09-10T08:15:30.123456789-04:00", "hostname": "2001:db8::1", "app": "kernel", "procid": nil},
		},
		{
			name: "local calendar timestamp", input: "2026-09-10 08:15:30 home cron: done",
			format: logmodel.FormatSyslogText, timestamp: "2026-09-10T05:15:30Z", message: "done",
			fields:      map[string]any{"time": "2026-09-10 08:15:30", "hostname": "home", "app": "cron", "procid": nil},
			diagnostics: []string{"timestamp_context_assumed"},
		},
		{
			name: "local empty message", input: "2026-09-10T08:15:30Z home cron:",
			format: logmodel.FormatSyslogText, timestamp: "2026-09-10T08:15:30Z",
			fields: map[string]any{"time": "2026-09-10T08:15:30Z", "hostname": "home", "app": "cron", "procid": nil},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := selectParser(tc.input, context, nil)
			r := d.Record
			if !d.IdentifiesLogs || r.SourceFormat != tc.format || r.Timestamp != tc.timestamp || r.Message != tc.message || r.Severity != tc.severity {
				t.Fatalf("decision = %#v", d)
			}
			if !reflect.DeepEqual(r.Fields, tc.fields) {
				t.Fatalf("fields = %#v, want %#v", r.Fields, tc.fields)
			}
			if len(r.Diagnostics) != len(tc.diagnostics) {
				t.Fatalf("diagnostics = %#v", r.Diagnostics)
			}
			for _, code := range tc.diagnostics {
				if !hasDiagnostic(r.Diagnostics, code) {
					t.Errorf("missing diagnostic %s", code)
				}
			}
		})
	}
}

func TestSyslogInvalidTimestampsRetainHeader(t *testing.T) {
	for _, input := range []string{
		"<34>1 2026-02-30T12:00:00Z home app - - - message",
		"<34>1 2026-09-10T12:00:00.123456789Z home app - - - message",
		"<34>1 2026-09-10T12:00:00+24:00 home app - - - message",
		"<34>1 2026-09-10T12:00:00+01:60 home app - - - message",
		"<34>1 2026-09-10t12:00:00z home app - - - message",
		"<34>Feb 30 12:00:00 home app: message",
		"2026-02-30T12:00:00Z home app: message",
	} {
		t.Run(input, func(t *testing.T) {
			d := selectParser(input, NewEngine(Options{}).context(), nil)
			if !d.IdentifiesLogs || d.Record.Timestamp != "" || d.Record.Fields["app"] != "app" || d.Record.Message != "message" || !hasDiagnostic(d.Record.Diagnostics, "invalid_timestamp") {
				t.Fatalf("decision = %#v", d)
			}
		})
	}
}

func TestMalformedSyslogRetainsWholeLine(t *testing.T) {
	for _, input := range []string{
		"<192>1 - h a - - - message", "<9999>1 - h a - - - message",
		"<01>1 - h a - - - message", "<1x>1 - h a - - - message", "<1",
		"<34>2 - h a - - - message", "<34>1 - h a - -",
		"<34>1  - h a - - - message", "<34>1 - h a - - malformed",
		`<34>1 - h a - - [meta x="unterminated]`,
		`<34>1 - h a - - [meta x="unescaped]"] message`,
		`<34>1 - h a - - [meta x="ok"trailer] message`,
		`<34>1 - h a - - [meta][meta] message`,
		`<34>1 - h a - - [meta]adjacent message`,
		`<34>1 - h a - - [ meta] message`,
		"<34>1 - h " + strings.Repeat("a", 49) + " - - - message",
		"<34>1 - h a - - [" + strings.Repeat("x", 33) + "] message",
		"<34>Sep 10 12:00:00 missing-tag",
	} {
		t.Run(input, func(t *testing.T) {
			d := selectParser(input, NewEngine(Options{}).context(), nil)
			if d.IdentifiesLogs || d.Record.SourceFormat != logmodel.FormatText || d.Record.Message != input || d.Record.Fields != nil || !hasDiagnostic(d.Record.Diagnostics, "malformed_syslog_fallback") {
				t.Fatalf("decision = %#v", d)
			}
			result, err := NewEngine(Options{}).Load(strings.NewReader(input))
			if err != nil || result.Kind != ResultRaw || result.Raw.Text != input {
				t.Fatalf("load = %#v, %v", result, err)
			}
		})
	}
}

func TestSyslogDoesNotCaptureUnrelatedTextOrPayloads(t *testing.T) {
	cases := []struct {
		input  string
		format logmodel.SourceFormat
	}{
		{"plain prose with host app: message", logmodel.FormatText},
		{"Sep 10 12:00:00 ordinary prose", logmodel.FormatText},
		{"2026-09-10T12:00:00Z level=info msg=literal", logmodel.FormatText},
		{"Sep 10 12:00:00 sshd[42]: hostname is missing", logmodel.FormatText},
		{"Sep 10 12:00:00 home app[broken]: not a supported tag", logmodel.FormatText},
		{`{"message":"<34>1 - h a - - - literal"}`, logmodel.FormatJSON},
		{`msg="<34>1 - h a - - - literal"`, logmodel.FormatLogfmt},
	}
	for _, tc := range cases {
		d := selectParser(tc.input, NewEngine(Options{}).context(), nil)
		if d.Record.SourceFormat != tc.format {
			t.Fatalf("%s: %#v", tc.input, d)
		}
	}
	d := selectParser(`<34>1 - h a - - - {"level":"error","message":"literal"}`, NewEngine(Options{}).context(), nil)
	if d.Record.Message != `{"level":"error","message":"literal"}` || d.Record.Severity != "fatal" {
		t.Fatalf("syslog payload was reinterpreted: %#v", d)
	}
}
