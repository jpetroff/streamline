package parse

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"streamline/internal/logmodel"
)

func TestBuiltInTextParsersMixedStreamingAndOffsets(t *testing.T) {
	frames := []string{
		"preamble\r\n",
		"\x1b[32m<30>1 2026-09-10T12:00:00Z home app 7 ID - ready\x1b[0m\r",
		`192.0.2.1 - - [10/Sep/2026:12:00:00 +0000] "GET / HTTP/1.1" 503 7` + "\r\n",
		"<30>Sep 10 12:00:00 home app[7]: legacy\n",
		"2026-09-10T12:00:00Z home app: local\n",
		`{"message":"json"}` + "\n",
		"msg=logfmt\n",
		"<999>bad\n",
		`192.0.2.1 - - [10/Sep/2026:12:00:00 +0000] "unterminated` + "\n",
		"plain tail",
	}
	formats := []logmodel.SourceFormat{
		logmodel.FormatText, logmodel.FormatSyslogRFC5424, logmodel.FormatHTTPAccess,
		logmodel.FormatSyslogRFC3164, logmodel.FormatSyslogText, logmodel.FormatJSON,
		logmodel.FormatLogfmt, logmodel.FormatText, logmodel.FormatText, logmodel.FormatText,
	}
	input := strings.Join(frames, "")
	options := Options{ReferenceTime: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	var emitted []CapturedRecord
	result, err := NewEngine(options).Stream(iotest.OneByteReader(strings.NewReader(input)), func(batch []CapturedRecord) {
		emitted = append(emitted, batch...)
	})
	if err != nil || result.Kind != ResultParsed || len(result.Parsed.Records) != len(frames) || string(result.Source) != input {
		t.Fatalf("mixed stream kind/count: %#v, %v", result, err)
	}
	if !reflect.DeepEqual(emitted, result.Parsed.Records) {
		t.Fatal("streamed records differ from final records")
	}
	position := 0
	for i, frame := range frames {
		record := result.Parsed.Records[i]
		if record.RawStart != position || record.RawEnd != position+len(frame) ||
			string(result.Source[record.RawStart:record.RawEnd]) != frame || record.Entry.SourceFormat != formats[i] {
			t.Fatalf("frame %d = %#v", i, record)
		}
		position += len(frame)
	}
	if !hasDiagnostic(emitted[7].Entry.Diagnostics, "malformed_syslog_fallback") ||
		!hasDiagnostic(emitted[8].Entry.Diagnostics, "malformed_http_access_fallback") {
		t.Fatal("malformed lines lost their diagnostics")
	}

	// Forced Text bypasses every new format, including malformed candidates.
	options.Text = true
	literal, err := NewEngine(options).Load(iotest.OneByteReader(strings.NewReader(input)))
	if err != nil || literal.Kind != ResultParsed || len(literal.Parsed.Records) != len(frames) {
		t.Fatal("text mode records changed")
	}
	for i, captured := range literal.Parsed.Records {
		want := sanitizeBytes([]byte(strings.TrimRight(frames[i], "\r\n")), false).text
		r := captured.Entry
		if r.SourceFormat != logmodel.FormatText || r.Fields != nil || r.Timestamp != "" || r.Severity != "" || r.Message != want {
			t.Fatalf("Text mode frame %d = %#v", i, r)
		}
	}
}

func TestBuiltInTextParsersPublishBeforeEOF(t *testing.T) {
	for _, line := range []string{
		"<30>1 - home app - - - ready",
		"<30>Sep 10 12:00:00 home app: ready",
		"2026-09-10T12:00:00Z home app: ready",
		`192.0.2.1 - - [10/Sep/2026:12:00:00 +0000] "GET / HTTP/1.1" 200 7`,
	} {
		t.Run(line, func(t *testing.T) {
			reader, writer := io.Pipe()
			t.Cleanup(func() { reader.Close(); writer.Close() })
			emitted := make(chan []CapturedRecord, 4)
			done := make(chan error, 1)
			go func() {
				_, err := NewEngine(Options{}).Stream(reader, func(batch []CapturedRecord) { emitted <- batch })
				done <- err
			}()
			half := len(line) / 2
			if _, err := io.WriteString(writer, "preamble\n"+line[:half]); err != nil {
				t.Fatal(err)
			}
			select {
			case batch := <-emitted:
				t.Fatalf("premature batch: %#v", batch)
			default:
			}
			if _, err := io.WriteString(writer, line[half:]+"\n"); err != nil {
				t.Fatal(err)
			}
			select {
			case batch := <-emitted:
				if len(batch) != 2 || batch[0].Entry.Message != "preamble" || batch[1].Entry.SourceFormat == logmodel.FormatText {
					t.Fatalf("batch = %#v", batch)
				}
			case <-time.After(time.Second):
				t.Fatal("record buffered until EOF")
			}
			writer.Close()
			if err := <-done; err != nil {
				t.Fatal(err)
			}

			sourceErr := errors.New("source failed")
			result, err := NewEngine(Options{}).Load(&failingReader{data: []byte(line), err: sourceErr})
			if !errors.Is(err, sourceErr) || result.Kind != ResultParsed || len(result.Parsed.Records) != 1 ||
				result.Parsed.Records[0].RawEnd != len(line) || result.Diagnostics[0].Diagnostic.Code != "input_read_error" {
				t.Fatalf("partial read failure: %#v, %v", result, err)
			}
		})
	}
}
