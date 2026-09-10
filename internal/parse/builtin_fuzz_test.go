package parse

import (
	"encoding/json"
	"testing"
	"time"

	"streamline/internal/logmodel"
)

// Parser candidates contain arbitrary client-supplied text. Check that every
// prefix/truncation remains safe to inspect and serializable for transport.
func FuzzBuiltInTextParsers(f *testing.F) {
	for _, seed := range []string{
		`<35>1 - host app 42 ID [meta x="escaped\\value" x="second"] payload`,
		"<30>Sep 10 12:00:00 home cron[42]: done",
		"2026-09-10T12:00:00Z home kernel: done",
		`192.0.2.1 - - [10/Sep/2026:12:00:00 +0000] "GET / HTTP/1.1" 503 10 "-" "agent\x20name"`,
	} {
		f.Add(seed)
	}
	context := NewEngine(Options{ReferenceTime: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}).context()
	f.Fuzz(func(t *testing.T, input string) {
		input = sanitizeBytes([]byte(input), false).text
		for _, access := range []bool{false, true} {
			var d parseDecision
			var candidate bool
			if access {
				d, candidate = parseHTTPAccess(input, nil)
			} else {
				d, candidate = parseSyslog(input, context, nil)
			}
			if !candidate {
				continue
			}
			if _, err := json.Marshal(d.Record); err != nil {
				t.Fatalf("non-JSON record: %v", err)
			}
			if !d.IdentifiesLogs && (d.Record.SourceFormat != logmodel.FormatText || d.Record.Message != input || d.Record.Fields != nil) {
				t.Fatalf("failed candidate lost input: %#v", d)
			}
		}
	})
}
