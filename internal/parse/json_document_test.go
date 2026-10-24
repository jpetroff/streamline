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

	"streamline/internal/logmodel"
)

func TestJSONDocumentObjectsAndOffsets(t *testing.T) {
	objects := []string{
		`{"message":"first 世界", "n":9007199254740993,"source":{"ip":"192.0.2.1"},"events":[{"message":"nested"}],"ok":true,"nothing":null}`,
		"{\r\n  \"msg\": \"second\\nline\",\r\n  \"ts\": 0, \"level\": \"WARNING\"\r\n}",
	}
	for _, input := range []string{
		" \r\n" + objects[0] + "\t\r\n",
		"[" + strings.Join(objects, ",") + "]",
		"\r\n[\r\n " + strings.Join(objects, " ,\r\n ") + "\r\n]\r\n",
		"[]", " [\r\n\t] ", "{}",
	} {
		t.Run(input, func(t *testing.T) {
			reader := &eofObservedReader{Reader: iotest.OneByteReader(strings.NewReader(input))}
			var emitted []CapturedRecord
			result, err := NewEngine(Options{Mode: ModeJSONDocument}).Stream(reader, func(batch []CapturedRecord) {
				if !reader.eof {
					t.Fatal("published before EOF")
				}
				emitted = append(emitted, batch...)
			})
			if err != nil || result.Kind != ResultParsed || result.Raw != nil || string(result.Source) != input {
				t.Fatalf("result = %#v, %v", result, err)
			}
			wantCount := 1
			if strings.HasPrefix(strings.TrimSpace(input), "[") {
				wantCount = 2
				if !strings.Contains(input, "{") {
					wantCount = 0
				}
			}
			if len(result.Parsed.Records) != wantCount || len(emitted) != wantCount {
				t.Fatalf("records = %#v, emitted = %#v", result.Parsed.Records, emitted)
			}
			for i, captured := range result.Parsed.Records {
				original := objects[i]
				if input == "{}" {
					original = "{}"
				}
				start := strings.Index(input, original)
				if captured.RawStart != start || captured.RawEnd != start+len(original) || string(result.Source[captured.RawStart:captured.RawEnd]) != original {
					t.Fatalf("object span = [%d,%d), want [%d,%d)", captured.RawStart, captured.RawEnd, start, start+len(original))
				}
				// Document framing must preserve precisely the same interpretation
				// as a single JSON line, including all source value types.
				var compact bytes.Buffer
				if err := json.Compact(&compact, []byte(original)); err != nil {
					t.Fatal(err)
				}
				line, err := NewEngine(Options{}).Load(&compact)
				if err != nil || !reflect.DeepEqual(captured.Entry, line.Parsed.Records[0].Entry) {
					t.Fatalf("normalization differs: %#v vs %#v (%v)", captured.Entry, line, err)
				}
				if !reflect.DeepEqual(emitted[i], captured) {
					t.Fatal("callback and final records differ")
				}
			}
		})
	}
}

type eofObservedReader struct {
	io.Reader
	eof bool
}

func (r *eofObservedReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		r.eof = true
	}
	return n, err
}

func TestJSONDocumentCleanupSpecializationAndFallbackMessages(t *testing.T) {
	input := `[
 {"\u001b[32mmessage\u001b[0m":"hello\u001b[31m 世界\u001b[0m\nnext","nested":{"value":"\u001b[31msafe"}},
 {"MESSAGE":"journal","__REALTIME_TIMESTAMP":"0","__CURSOR":"c","PRIORITY":"4"},
 {"message":{"text":"structured"}},
 {"items":[{"message":"do not unwrap"}]},
 {"message":"{\"message\":\"do not reparse\"}","created_at":"2026-10-24T13:10:25Z","start_at":"2026-10-24T13:10:24Z"}
]`
	result, err := NewEngine(Options{Mode: ModeJSONDocument}).Load(strings.NewReader(input))
	if err != nil || result.Kind != ResultParsed || len(result.Parsed.Records) != 5 {
		t.Fatalf("result = %#v, %v", result, err)
	}
	records := result.Parsed.Records
	first := records[0].Entry
	if first.Message != "hello 世界\nnext" || first.Fields["nested"].(map[string]any)["value"] != "safe" || !hasDiagnostic(first.Diagnostics, "terminal_controls_removed") {
		t.Fatalf("cleanup = %#v", first)
	}
	journal := records[1].Entry
	if journal.SourceFormat != logmodel.FormatJournaldJSON || journal.Message != "journal" || journal.Timestamp != "1970-01-01T00:00:00Z" || journal.Severity != "warn" {
		t.Fatalf("journal = %#v", journal)
	}
	if !records[2].Entry.MessageIsJSON || !hasDiagnostic(records[2].Entry.Diagnostics, "non_text_message") || !records[3].Entry.MessageIsJSON || !hasDiagnostic(records[3].Entry.Diagnostics, "missing_message") {
		t.Fatal("structured display fallback changed")
	}
	last := records[4].Entry
	if last.Message != `{"message":"do not reparse"}` || last.Timestamp != "" || last.Severity != "" {
		t.Fatalf("unexpected interpretation = %#v", last)
	}

	invalidUTF8 := "{\"message\":\"bad\xff byte\"}"
	result, err = NewEngine(Options{Mode: ModeJSONDocument}).Load(strings.NewReader(invalidUTF8))
	if err != nil || result.Kind != ResultParsed || result.Parsed.Records[0].Entry.Message != "bad\ufffd byte" || !hasDiagnostic(result.Parsed.Records[0].Entry.Diagnostics, "invalid_utf8_replaced") {
		t.Fatalf("UTF-8 cleanup = %#v, %v", result, err)
	}
}

func TestJSONDocumentFailureIsAtomicRaw(t *testing.T) {
	for _, tc := range []struct{ input, code string }{
		{"", "malformed_json_document"},
		{" \r\n", "malformed_json_document"},
		{"[", "malformed_json_document"},
		{`[{"message":"valid"},`, "malformed_json_document"},
		{`[{"message":"valid"}, {"message":}]`, "malformed_json_document"},
		{`[{"message":"valid"},]`, "malformed_json_document"},
		{`[{"message":"valid"}] trailing`, "malformed_json_document"},
		{`{} {}`, "malformed_json_document"},
		{"{}\n{}\n", "malformed_json_document"},
		{"\x1b[32m{}\x1b[0m", "malformed_json_document"},
		{`{"message":"literal` + "\x1b" + `"}`, "malformed_json_document"},
		{"null", "unsupported_json_document"},
		{"42", "unsupported_json_document"},
		{"true", "unsupported_json_document"},
		{`"text"`, "unsupported_json_document"},
		{`[{"message":"valid"}, null]`, "unsupported_json_document"},
		{`[{}, 42]`, "unsupported_json_document"},
		{`[{}, "text"]`, "unsupported_json_document"},
		{`[{}, []]`, "unsupported_json_document"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			result, err := NewEngine(Options{Mode: ModeJSONDocument}).Stream(iotest.OneByteReader(strings.NewReader(tc.input)), func([]CapturedRecord) {
				t.Fatal("published records from invalid document")
			})
			if err != nil || result.Kind != ResultRaw || result.Parsed != nil || string(result.Source) != tc.input || result.Raw.Text != sanitizeBytes([]byte(tc.input), true).text {
				t.Fatalf("raw result = %#v, %v", result, err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Diagnostic.Code != tc.code || result.Diagnostics[0].RawStart != 0 || result.Diagnostics[0].RawEnd != len(tc.input) {
				t.Fatalf("load diagnostics = %#v", result.Diagnostics)
			}
		})
	}
}

func TestJSONDocumentReadCompletion(t *testing.T) {
	inputErr := errors.New("interrupted")
	for _, input := range []string{`[{"message":"complete"}]`, `[{"message":"complete"},`} {
		result, err := NewEngine(Options{Mode: ModeJSONDocument}).Stream(&failingReader{data: []byte(input), err: inputErr}, func([]CapturedRecord) {
			t.Fatal("published interrupted document")
		})
		if !errors.Is(err, inputErr) || result.Kind != ResultRaw || result.Raw.Text != input || result.Parsed != nil {
			t.Fatalf("read error result = %#v, %v", result, err)
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Diagnostic.Code != "input_read_error" || result.Diagnostics[0].RawStart != len(input) || result.Diagnostics[0].RawEnd != len(input) {
			t.Fatalf("diagnostics = %#v", result.Diagnostics)
		}
	}
	result, err := NewEngine(Options{Mode: ModeJSONDocument}).Load(&failingReader{data: []byte(`{"message":"ok"}`), err: io.EOF})
	if err != nil || result.Kind != ResultParsed || len(result.Parsed.Records) != 1 {
		t.Fatalf("bytes with EOF = %#v, %v", result, err)
	}
}

func TestJSONDocumentCrowdSec(t *testing.T) {
	path := "testdata/crowdsec-document.json"
	wantCount := 2
	if full := os.Getenv("STREAMLINE_CROWDSEC_DOCUMENT"); full != "" {
		path, wantCount = full, 50
	}
	input, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var originals []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&originals); err != nil {
		t.Fatal(err)
	}
	result, err := NewEngine(Options{Mode: ModeJSONDocument}).Load(iotest.HalfReader(bytes.NewReader(input)))
	if err != nil || result.Kind != ResultParsed || len(result.Parsed.Records) != wantCount {
		t.Fatalf("CrowdSec result = %#v, %v", result, err)
	}
	for i, captured := range result.Parsed.Records {
		r := captured.Entry
		if !reflect.DeepEqual(r.Fields, originals[i]) || r.Message != originals[i]["message"] || r.Timestamp != "" || r.Severity != "" || r.SourceFormat != logmodel.FormatJSON {
			t.Fatalf("alert %d changed: %#v", i, r)
		}
		var raw map[string]any
		if err := json.Unmarshal(result.Source[captured.RawStart:captured.RawEnd], &raw); err != nil {
			t.Fatalf("alert %d span is not an object: %v", i, err)
		}
	}
}
