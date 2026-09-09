package parse

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"streamline/internal/logmodel"
)

func TestLogfmtValuesAndNormalization(t *testing.T) {
	context := NewEngine(Options{}).context()
	cases := []struct {
		name, input, message, timestamp, severity string
		fields                                    map[string]any
		diagnostics                               []string
		messageIsJSON                             bool
	}{
		{
			name: "CrowdSec", input: `time="2026-01-03T18:07:22+02:00" level=warning msg="blocked request" module=db`,
			message: "blocked request", timestamp: "2026-01-03T16:07:22Z", severity: "warn",
			fields: map[string]any{"time": "2026-01-03T18:07:22+02:00", "level": "warning", "msg": "blocked request", "module": "db"},
		},
		{
			name: "Unicode whitespace is a literal value", input: "msg=hello\u00a0",
			message: "hello\u00a0", fields: map[string]any{"msg": "hello\u00a0"},
		},
		{
			name: "literal strings and keys", input: " \tstatus=403 enabled=true quoted=\"403\" empty= quotedEmpty=\"\" nested.key=value url=https://example.test/?a=b=c\t",
			message:       `{"empty":"","enabled":"true","nested.key":"value","quoted":"403","quotedEmpty":"","status":"403","url":"https://example.test/?a=b=c"}`,
			messageIsJSON: true, diagnostics: []string{"missing_message"},
			fields: map[string]any{"status": "403", "enabled": "true", "quoted": "403", "empty": "", "quotedEmpty": "", "nested.key": "value", "url": "https://example.test/?a=b=c"},
		},
		{
			name: "Go escaping", input: `msg="hello \"世界\"\nnext\tline \\ \u263a \x41 \101" command="journalctl --unit=docker* NAME=server"`,
			message: "hello \"世界\"\nnext\tline \\ ☺ A A",
			fields:  map[string]any{"msg": "hello \"世界\"\nnext\tline \\ ☺ A A", "command": "journalctl --unit=docker* NAME=server"},
		},
		{
			name: "decoded controls", input: `msg="\x1b[31mred\x1b[0m\x00\xff"`, message: "red�",
			fields: map[string]any{"msg": "red�"}, diagnostics: []string{"terminal_controls_removed", "invalid_utf8_replaced"},
		},
		{
			name: "duplicate last wins", input: `level=info level=error msg=first msg=last`, message: "last", severity: "error",
			fields: map[string]any{"level": "error", "msg": "last"}, diagnostics: []string{"duplicate_logfmt_key"},
		},
		{
			name: "alias precedence", input: `message="" msg=second log=third severity=error level=info timestamp="2026-01-01T00:00:00Z" time=bad`,
			message: "", severity: "error", timestamp: "2026-01-01T00:00:00Z",
			fields: map[string]any{"message": "", "msg": "second", "log": "third", "severity": "error", "level": "info", "timestamp": "2026-01-01T00:00:00Z", "time": "bad"},
		},
		{
			name: "invalid aliases fall through", input: `msg=ok severity=bad level=trace timestamp=bad time="2026-01-01 00:00:00"`,
			message: "ok", severity: "debug", timestamp: "2026-01-01T00:00:00Z",
			fields:      map[string]any{"msg": "ok", "severity": "bad", "level": "trace", "timestamp": "bad", "time": "2026-01-01 00:00:00"},
			diagnostics: []string{"invalid_timestamp", "invalid_severity", "timestamp_context_assumed"},
		},
		{
			name: "numeric aliases stay strings", input: `msg=ok ts=1000 priority=3`, message: "ok",
			fields: map[string]any{"msg": "ok", "ts": "1000", "priority": "3"}, diagnostics: []string{"invalid_timestamp", "invalid_severity"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := selectParser(tc.input, context, nil)
			r := decision.Record
			if !decision.IdentifiesLogs || r.SourceFormat != logmodel.FormatLogfmt || r.Message != tc.message || r.Timestamp != tc.timestamp || r.Severity != tc.severity || r.MessageIsJSON != tc.messageIsJSON {
				t.Fatalf("decision = %#v", decision)
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

func TestMalformedLogfmtRetainsWholeLine(t *testing.T) {
	for _, input := range []string{
		`msg="unterminated`, `msg="trailing\`, `msg="bad\q"`, `msg="ok"tail=x`,
		`msg=un"quoted`, `msg=ok trailing`, `msg=ok =value`, `=value`, `"key"=value`,
		`msg="\xff" msg=second trailing`,
	} {
		t.Run(input, func(t *testing.T) {
			d := selectParser(input, NewEngine(Options{}).context(), nil)
			if d.IdentifiesLogs || d.Record.SourceFormat != logmodel.FormatText || d.Record.Message != input || d.Record.Fields != nil || len(d.Record.Diagnostics) != 1 || !hasDiagnostic(d.Record.Diagnostics, "malformed_logfmt_fallback") {
				t.Fatalf("decision = %#v", d)
			}
			result, err := NewEngine(Options{}).Load(strings.NewReader(input))
			if err != nil || result.Kind != ResultRaw || result.Raw.Text != input {
				t.Fatalf("load = %#v, %v", result, err)
			}
		})
	}
}

func TestCrowdSecFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/crowdsec.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input               string
		Fields              map[string]any
		Timestamp, Severity string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 18 {
		t.Fatalf("fixture cases = %d", len(cases))
	}
	var input strings.Builder
	for _, tc := range cases {
		input.WriteString(tc.Input + "\n")
	}
	result, err := NewEngine(Options{}).Load(strings.NewReader(input.String()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ResultParsed || len(result.Parsed.Records) != len(cases) {
		t.Fatalf("unexpected result kind/count: %s", result.Kind)
	}
	for i, tc := range cases {
		captured := result.Parsed.Records[i]
		r := captured.Entry
		if r.SourceFormat != logmodel.FormatLogfmt || !reflect.DeepEqual(r.Fields, tc.Fields) || r.Message != tc.Fields["msg"] || r.Timestamp != tc.Timestamp || r.Severity != tc.Severity || len(r.Diagnostics) != 0 {
			t.Fatalf("fixture %d: %#v", i, r)
		}
		if string(result.Source[captured.RawStart:captured.RawEnd]) != tc.Input+"\n" {
			t.Fatalf("fixture %d raw span", i)
		}
	}
}

func TestSuppliedCrowdSecDatasetWhenAvailable(t *testing.T) {
	file, err := os.Open("../../dataset/crowdsec.log")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("local CrowdSec dataset is not present")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result, err := NewEngine(Options{}).Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ResultParsed || len(result.Parsed.Records) != 136232 {
		t.Fatalf("unexpected CrowdSec result kind/count: %s", result.Kind)
	}
	end := 0
	for i, captured := range result.Parsed.Records {
		r := captured.Entry
		if r.SourceFormat != logmodel.FormatLogfmt || r.Fields["msg"] != r.Message || len(r.Diagnostics) != 0 {
			t.Fatalf("record %d: %#v", i, r)
		}
		sourceTime, ok := r.Fields["time"].(string)
		if !ok {
			t.Fatalf("record %d missing source time", i)
		}
		stamp, err := time.Parse(time.RFC3339Nano, sourceTime)
		if err != nil || r.Timestamp != stamp.UTC().Format(time.RFC3339Nano) {
			t.Fatalf("record %d timestamp = %s", i, r.Timestamp)
		}
		if r.Severity != map[any]string{"info": "info", "warning": "warn", "error": "error", "fatal": "fatal"}[r.Fields["level"]] {
			t.Fatalf("record %d severity = %s", i, r.Severity)
		}
		for key, value := range r.Fields {
			if _, ok := value.(string); !ok {
				t.Fatalf("record %d field %s has type %T", i, key, value)
			}
		}
		if captured.RawStart != end || captured.RawEnd <= end {
			t.Fatalf("record %d noncontiguous raw span", i)
		}
		line := string(bytes.TrimRight(result.Source[captured.RawStart:captured.RawEnd], "\r\n"))
		if !strings.HasPrefix(line, `time="`+sourceTime+`" level=`) {
			t.Fatalf("record %d incorrect raw span", i)
		}
		end = captured.RawEnd
	}
	if end != len(result.Source) {
		t.Fatal("raw spans did not cover full source")
	}
}

func TestLogfmtChunkBoundariesAndPartialReadError(t *testing.T) {
	input := "preamble\r\n" + `time="2026-01-03T18:07:22+02:00" level=info msg="first"` + "\r" + `msg="second\"quoted"` + "\n" + "msg=" + strings.Repeat("x", 70*1024)
	result, err := NewEngine(Options{}).Load(iotest.OneByteReader(strings.NewReader(input)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ResultParsed || len(result.Parsed.Records) != 4 || string(result.Source) != input {
		t.Fatal("chunked input was not preserved")
	}
	want := []string{"preamble", "first", `second"quoted`, strings.Repeat("x", 70*1024)}
	end := 0
	for i, r := range result.Parsed.Records {
		if r.RawStart != end || r.Entry.Message != want[i] {
			t.Fatalf("record %d = %#v", i, r)
		}
		end = r.RawEnd
	}
	if end != len(input) {
		t.Fatal("final raw span does not include partial line")
	}
	sourceErr := errors.New("source failed")
	partial, err := NewEngine(Options{}).Load(&failingReader{data: []byte(`msg=partial`), err: sourceErr})
	if !errors.Is(err, sourceErr) || partial.Kind != ResultParsed || partial.Parsed.Records[0].Entry.Message != "partial" || partial.Diagnostics[0].Diagnostic.Code != "input_read_error" {
		t.Fatalf("partial result = %#v, %v", partial, err)
	}
}

func TestLogfmtStreamsPreambleBeforeEOF(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() { reader.Close(); writer.Close() })
	emitted := make(chan []CapturedRecord, 4)
	done := make(chan error, 1)
	go func() {
		_, err := NewEngine(Options{}).Stream(reader, func(batch []CapturedRecord) { emitted <- batch })
		done <- err
	}()
	if _, err := io.WriteString(writer, "preamble\nmsg=\"half"); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-emitted:
		t.Fatalf("premature batch: %#v", batch)
	default:
	}
	if _, err := io.WriteString(writer, " complete\"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-emitted:
		if len(batch) != 2 || batch[0].Entry.Message != "preamble" || batch[1].Entry.Message != "half complete" || batch[1].Entry.SourceFormat != logmodel.FormatLogfmt {
			t.Fatalf("batch = %#v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("logfmt buffered until EOF")
	}
	writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
