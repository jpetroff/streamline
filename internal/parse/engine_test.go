package parse

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"streamline/internal/logmodel"
)

func TestLoadJournaldJSONAndOfficialFieldShapes(t *testing.T) {
	input, err := os.ReadFile("testdata/journald.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewEngine(Options{}).Load(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.Source, input) {
		t.Fatal("raw input was not preserved")
	}
	if len(result.Parsed.Records) != 2 {
		t.Fatalf("record count = %d, want 2", len(result.Parsed.Records))
	}

	first := result.Parsed.Records[0]
	firstLineEnd := bytes.IndexByte(input, '\n') + 1
	if first.RawStart != 0 || first.RawEnd != firstLineEnd || !bytes.Equal(result.Source[first.RawStart:first.RawEnd], input[:firstLineEnd]) {
		t.Fatalf("first raw span = [%d:%d], want [0:%d]", first.RawStart, first.RawEnd, firstLineEnd)
	}
	if first.Entry.SourceFormat != logmodel.FormatJournaldJSON {
		t.Fatalf("format = %q", first.Entry.SourceFormat)
	}
	if first.Entry.Message != `time="2026-08-28T23:24:53+03:00" level=info msg="kept intact"` {
		t.Fatalf("message = %q", first.Entry.Message)
	}
	if first.Entry.Timestamp != "2026-08-28T20:24:53.536796Z" {
		t.Fatalf("timestamp = %q", first.Entry.Timestamp)
	}
	if first.Entry.Severity != "info" {
		t.Fatalf("severity = %q", first.Entry.Severity)
	}
	if value, exists := first.Entry.Fields["NESTED"]; !exists || value != nil {
		t.Fatalf("null field = %#v, exists %v", value, exists)
	}

	second := result.Parsed.Records[1].Entry
	if second.Message != "hi!" {
		t.Fatalf("binary message = %q", second.Message)
	}
	if second.Timestamp != "1970-01-01T00:00:01Z" || second.Severity != "warn" {
		t.Fatalf("second canonical values = timestamp %q severity %q", second.Timestamp, second.Severity)
	}
	messageBytes, ok := second.Fields["MESSAGE"].([]any)
	if !ok || len(messageBytes) != 12 {
		t.Fatalf("MESSAGE field = %#v", second.Fields["MESSAGE"])
	}
	if _, ok := messageBytes[0].(json.Number); !ok {
		t.Fatalf("binary JSON number type = %T", messageBytes[0])
	}
	if !hasDiagnostic(second.Diagnostics, "terminal_controls_removed") || !hasDiagnostic(second.Diagnostics, "multiple_journal_values") {
		t.Fatalf("second diagnostics = %#v", second.Diagnostics)
	}
}

func TestLoadMixedJSONPlainTextAndTerminalTranscript(t *testing.T) {
	location := time.FixedZone("example", 3*60*60)
	reference := time.Date(2027, time.January, 1, 0, 0, 30, 0, location)
	engine := NewEngine(Options{DefaultLocation: location, ReferenceTime: reference})

	generic := `{"level":"info","ts":1788455223.5670338,"msg":"handled request","request":{"method":"GET"},"ok":true,"missing":null,"tags":["a",2]}`
	coloredPlain := "\x1b[1;31m2026-09-02 19:52:06.408101497 plain failure\x1b[0m"
	pagerFiller := "\x1b[1m~\x1b[0m"
	pagerStatus := "\x1b[7mlines 1-10/10 (END)\x1b[27m"
	syslog := "Dec 31 23:59:59 host process: finished"
	truncated := "{\"message\":\"cut\"\x1b[7m>\x1b[27m"
	input := generic + "\r\n" + coloredPlain + "\r" + pagerFiller + "\r\n" + pagerStatus + "\n" + syslog + "\n" + truncated

	result, err := engine.Load(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parsed.Records) != 4 {
		t.Fatalf("record count = %d, want 4", len(result.Parsed.Records))
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("load diagnostics = %#v", result.Diagnostics)
	}

	jsonRecord := result.Parsed.Records[0].Entry
	if jsonRecord.SourceFormat != logmodel.FormatJSON || jsonRecord.Message != "handled request" || jsonRecord.Severity != "info" {
		t.Fatalf("generic record = %#v", jsonRecord)
	}
	if jsonRecord.Timestamp != "2026-09-03T17:07:03.5670338Z" {
		t.Fatalf("generic timestamp = %q", jsonRecord.Timestamp)
	}
	if jsonRecord.Fields["ts"].(json.Number).String() != "1788455223.5670338" {
		t.Fatalf("numeric field = %#v", jsonRecord.Fields["ts"])
	}
	if jsonRecord.Fields["request"].(map[string]any)["method"] != "GET" {
		t.Fatalf("nested fields = %#v", jsonRecord.Fields)
	}

	plain := result.Parsed.Records[1]
	if plain.Entry.SourceFormat != logmodel.FormatText || plain.Entry.Message != "plain failure" {
		t.Fatalf("plain record = %#v", plain.Entry)
	}
	if plain.Entry.Timestamp != "2026-09-02T16:52:06.408101497Z" {
		t.Fatalf("plain timestamp = %q", plain.Entry.Timestamp)
	}
	if !hasDiagnostic(plain.Entry.Diagnostics, "terminal_controls_removed") || !hasDiagnostic(plain.Entry.Diagnostics, "timestamp_context_assumed") {
		t.Fatalf("plain diagnostics = %#v", plain.Entry.Diagnostics)
	}
	if result.Source[plain.RawEnd-1] != '\r' {
		t.Fatalf("plain raw span does not include lone CR: %q", result.Source[plain.RawStart:plain.RawEnd])
	}

	syslogRecord := result.Parsed.Records[2].Entry
	if syslogRecord.Timestamp != "2026-12-31T20:59:59Z" || syslogRecord.Message != "finished" || syslogRecord.SourceFormat != logmodel.FormatSyslogText || syslogRecord.Fields["hostname"] != "host" || syslogRecord.Fields["app"] != "process" {
		t.Fatalf("syslog record = %#v", syslogRecord)
	}

	broken := result.Parsed.Records[3].Entry
	if broken.SourceFormat != logmodel.FormatText || !hasDiagnostic(broken.Diagnostics, "malformed_json_fallback") || !hasDiagnostic(broken.Diagnostics, "terminal_truncated") {
		t.Fatalf("truncated record = %#v", broken)
	}
}

func TestTerminalControlsCanBeRemovedBeforeJSONRecognition(t *testing.T) {
	input := "{\"\x1b[32mMESSAGE\x1b[0m\":\"\x1b[32mok\x1b[0m\",\"__REALTIME_TIMESTAMP\":\"0\",\"__CURSOR\":\"cursor\"}\n"
	result, err := NewEngine(Options{}).Load(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parsed.Records) != 1 {
		t.Fatalf("record count = %d", len(result.Parsed.Records))
	}
	record := result.Parsed.Records[0].Entry
	if record.SourceFormat != logmodel.FormatJournaldJSON || record.Message != "ok" {
		t.Fatalf("record = %#v", record)
	}
	if _, exists := record.Fields["MESSAGE"]; !exists || !hasDiagnostic(record.Diagnostics, "terminal_controls_removed") {
		t.Fatalf("cleaned fields or diagnostics = %#v", record)
	}
}

func TestTimestampContextAndJSONFallbacks(t *testing.T) {
	location := time.FixedZone("west", -5*60*60)
	engine := NewEngine(Options{
		DefaultLocation: location,
		ReferenceTime:   time.Date(2026, time.July, 1, 0, 0, 0, 0, location),
	})
	input := strings.Join([]string{
		"2026-07-02 03:04:05 no-zone",
		`{"message":"","time":"2026-07-02T03:04:05-05:00","level":"WARNING"}`,
		`{"value":true}`,
		`[1,2,3]`,
	}, "\n")
	result, err := engine.Load(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parsed.Records) != 4 {
		t.Fatalf("record count = %d", len(result.Parsed.Records))
	}
	if result.Parsed.Records[0].Entry.Timestamp != "2026-07-02T08:04:05Z" || result.Parsed.Records[0].Entry.Message != "no-zone" {
		t.Fatalf("context timestamp = %#v", result.Parsed.Records[0].Entry)
	}
	if result.Parsed.Records[1].Entry.Message != "" || result.Parsed.Records[1].Entry.Severity != "warn" || result.Parsed.Records[1].Entry.Timestamp != "2026-07-02T08:04:05Z" {
		t.Fatalf("JSON aliases = %#v", result.Parsed.Records[1].Entry)
	}
	if !hasDiagnostic(result.Parsed.Records[2].Entry.Diagnostics, "missing_message") {
		t.Fatalf("missing-message fallback = %#v", result.Parsed.Records[2].Entry)
	}
	if !hasDiagnostic(result.Parsed.Records[3].Entry.Diagnostics, "unsupported_json_root") {
		t.Fatalf("array fallback = %#v", result.Parsed.Records[3].Entry)
	}
}

func TestInvalidUTF8LongLinesAndFinalPartialRecord(t *testing.T) {
	long := strings.Repeat("x", 70*1024)
	input := append([]byte("2026-09-04T12:00:00Z "+long+"\ninvalid-"), 0xff)
	result, err := NewEngine(Options{}).Load(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parsed.Records) != 2 || result.Parsed.Records[0].Entry.Message != long {
		t.Fatalf("long-line result sizes = records %d first length %d", len(result.Parsed.Records), len(result.Parsed.Records[0].Entry.Message))
	}
	if result.Parsed.Records[1].RawEnd != len(input) || result.Parsed.Records[1].Entry.Message != "invalid-�" {
		t.Fatalf("final record = %#v", result.Parsed.Records[1])
	}
	if !hasDiagnostic(result.Parsed.Records[1].Entry.Diagnostics, "invalid_utf8_replaced") {
		t.Fatalf("final diagnostics = %#v", result.Parsed.Records[1].Entry.Diagnostics)
	}
}

type failingReader struct {
	data []byte
	done bool
	err  error
}

func (reader *failingReader) Read(destination []byte) (int, error) {
	if reader.done {
		return 0, reader.err
	}
	reader.done = true
	return copy(destination, reader.data), reader.err
}

func TestReaderFailureReturnsRawPartialResult(t *testing.T) {
	sourceError := errors.New("source failed")
	result, err := NewEngine(Options{}).Load(&failingReader{data: []byte("partial"), err: sourceError})
	if !errors.Is(err, sourceError) {
		t.Fatalf("error = %v", err)
	}
	if result.Kind != ResultRaw || result.Raw == nil || result.Raw.Text != "partial" || result.Parsed != nil {
		t.Fatalf("partial result = %#v", result)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Diagnostic.Code != "input_read_error" {
		t.Fatalf("load diagnostics = %#v", result.Diagnostics)
	}
}

func TestLoadFallsBackToDisplaySafeRawText(t *testing.T) {
	input := []byte("Report title\r\n\tvalue \x1b[31mred\x1b[0m\r")
	result, err := NewEngine(Options{}).Load(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ResultRaw || result.Raw == nil || result.Parsed != nil {
		t.Fatalf("result variant = %#v", result)
	}
	if result.Raw.Text != "Report title\n\tvalue red\n" {
		t.Fatalf("raw text = %q", result.Raw.Text)
	}
	if !bytes.Equal(result.Source, input) {
		t.Fatal("exact source was not retained")
	}
}

func TestStreamBuffersPreambleThenEmitsRecognizedRecordsBeforeEOF(t *testing.T) {
	reader, writer := io.Pipe()
	emitted := make(chan []CapturedRecord, 2)
	done := make(chan *LoadResult, 1)
	go func() {
		result, err := NewEngine(Options{}).Stream(reader, func(records []CapturedRecord) {
			emitted <- records
		})
		if err != nil {
			t.Errorf("stream error: %v", err)
		}
		done <- result
	}()

	if _, err := writer.Write([]byte("command preamble\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case records := <-emitted:
		t.Fatalf("unrecognized preamble emitted early: %#v", records)
	case <-time.After(20 * time.Millisecond):
	}

	if _, err := writer.Write([]byte("{\"message\":\"first\"}\r\n")); err != nil {
		t.Fatal(err)
	}
	first := <-emitted
	if len(first) != 2 || first[0].Entry.Message != "command preamble" || first[1].Entry.Message != "first" {
		t.Fatalf("first progressive batch = %#v", first)
	}

	if _, err := writer.Write([]byte("2026-09-04T12:00:00Z second\n")); err != nil {
		t.Fatal(err)
	}
	second := <-emitted
	if len(second) != 1 || second[0].Entry.Message != "second" {
		t.Fatalf("second progressive batch = %#v", second)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.Kind != ResultParsed || result.Parsed == nil || result.Raw != nil || len(result.Parsed.Records) != 3 {
		t.Fatalf("final result = %#v", result)
	}
}

func TestSanitizerRemovesControlStringsAndKeepsVisibleText(t *testing.T) {
	input := []byte("a\x1b]8;;https://example.com\ab\x1b]8;;\x1b\\c\x1bPignored\x1b\\d\xc2\x9b31me")
	cleaned := sanitizeBytes(input, true)
	if cleaned.text != "abcde" || !cleaned.hadTerminal || cleaned.invalidUTF8 {
		t.Fatalf("cleaned = %#v", cleaned)
	}
}

func TestJournaldMissingValuesAndInvalidSourceTimestampFallback(t *testing.T) {
	input := strings.Join([]string{
		`{"MESSAGE":null,"_SOURCE_REALTIME_TIMESTAMP":"bad","__REALTIME_TIMESTAMP":"1000000","__CURSOR":"one"}`,
		`{"__REALTIME_TIMESTAMP":"2000000","__CURSOR":"two"}`,
	}, "\n")
	result, err := NewEngine(Options{}).Load(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parsed.Records) != 2 {
		t.Fatalf("record count = %d", len(result.Parsed.Records))
	}
	nonText := result.Parsed.Records[0].Entry
	if nonText.Timestamp != "1970-01-01T00:00:01Z" ||
		!hasDiagnostic(nonText.Diagnostics, "non_text_message") ||
		!hasDiagnostic(nonText.Diagnostics, "invalid_timestamp") {
		t.Fatalf("non-text journal record = %#v", nonText)
	}
	missing := result.Parsed.Records[1].Entry
	if missing.Message == "" || !hasDiagnostic(missing.Diagnostics, "missing_message") {
		t.Fatalf("missing-message journal record = %#v", missing)
	}
}

func TestSuppliedReferenceDatasetsWhenAvailable(t *testing.T) {
	plain, err := os.Open("../../dataset/journald_logs_plain.txt")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("local reference datasets are not present")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()

	plainResult, err := NewEngine(Options{}).Load(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(plainResult.Parsed.Records) != 10 {
		t.Fatalf("plain journal record count = %d, want 10", len(plainResult.Parsed.Records))
	}
	for _, record := range plainResult.Parsed.Records {
		if record.Entry.SourceFormat != logmodel.FormatJournaldJSON {
			t.Fatalf("plain journal format = %q", record.Entry.SourceFormat)
		}
	}

	terminal, err := os.Open("../../dataset/journald_logs_terminal_output.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	terminalResult, err := NewEngine(Options{}).Load(terminal)
	if err != nil {
		t.Fatal(err)
	}
	// Local captures can contain truncated records without standalone pager frames.
	// Skipped-pager diagnostics are covered by the deterministic mixed-input test.
	if terminalResult.Kind != ResultRaw || terminalResult.Raw == nil {
		t.Fatalf("terminal result did not fall back to raw: kind=%s", terminalResult.Kind)
	}
	for _, character := range terminalResult.Raw.Text {
		if character == '\x1b' || character == '\x7f' || (character >= 0x80 && character <= 0x9f) {
			t.Fatalf("unsafe terminal control remained in %q", terminalResult.Raw.Text)
		}
	}
}

func TestTextModePublishesSanitizedLinesWithoutDetection(t *testing.T) {
	reader, writer := io.Pipe()
	records := make(chan []CapturedRecord, 4)
	done := make(chan error, 1)
	go func() {
		_, err := NewEngine(Options{Text: true}).Stream(reader, func(batch []CapturedRecord) { records <- batch })
		done <- err
	}()
	if _, err := io.WriteString(writer, "\x1b[31mplain\x1b[0m\n\n{\"message\":\"json stays text\"}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-records:
		if len(batch) != 2 || batch[0].Entry.Message != "plain" || batch[1].Entry.SourceFormat != logmodel.FormatText || batch[1].Entry.Message != `{"message":"json stays text"}` {
			t.Fatalf("batch = %#v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("text was buffered until EOF")
	}
	io.WriteString(writer, "final partial")
	writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if batch := <-records; len(batch) != 1 || batch[0].Entry.Message != "final partial" {
		t.Fatalf("final = %#v", batch)
	}
	reader.Close()
}
