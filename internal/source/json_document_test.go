//go:build linux || darwin

package source

import (
	"io"
	"strings"
	"testing"

	"streamline/internal/logmodel"
	"streamline/internal/parse"
	"streamline/internal/query"
)

func TestJSONDocumentCommandAndStdin(t *testing.T) {
	m := New()
	defer m.Close()
	input := "[\n {\"message\":\"first\",\"source\":{\"ip\":\"192.0.2.1\"}},\n {\"message\":\"second\",\"count\":2}\n]"
	m.StartStdin(io.NopCloser(strings.NewReader(input)), parse.ModeJSONDocument)
	info := create(t, m, "printf '%s' '"+input+"'", "json-document")
	for _, id := range []string{info.ID, "stdin"} {
		finished := completed(t, m, id)
		if finished.Mode != "json-document" || finished.State != "exited" || finished.Session.InputKind != query.InputRecords {
			t.Fatalf("source = %#v", finished)
		}
		got := rows(t, m, id)
		if len(got) != 2 || got[0].Message != "first" || got[1].Message != "second" || got[0].SourceFormat != logmodel.FormatJSON || got[0].Fields["source"].(map[string]any)["ip"] != "192.0.2.1" {
			t.Fatalf("rows = %#v", got)
		}
	}
}

func TestStdinTextModeAndDescriptor(t *testing.T) {
	m := New()
	defer m.Close()
	input := `{"message":"literal"}`
	m.StartStdin(io.NopCloser(strings.NewReader(input)), parse.ModeText)
	finished := completed(t, m, "stdin")
	got := rows(t, m, "stdin")
	if finished.Mode != "text" || len(got) != 1 || got[0].Message != input || got[0].SourceFormat != logmodel.FormatText || got[0].Fields != nil {
		t.Fatalf("source = %#v, rows = %#v", finished, got)
	}
}

func TestJSONDocumentMalformedCommandRetainsRawOutput(t *testing.T) {
	m := New()
	defer m.Close()
	info := create(t, m, `printf '%s' '[{"message":"valid"}, null]'`, "json-document")
	finished := completed(t, m, info.ID)
	if finished.State != "exited" || finished.Session.InputKind != query.InputRaw || finished.Session.InputStatus != query.InputEOF {
		t.Fatalf("source = %#v", finished)
	}
	q, _ := m.Queries(info.ID)
	page, err := q.Raw(t.Context(), finished.Session.GenerationID, 0, 4)
	if err != nil || strings.Join(page.Chunks, "") != `[{"message":"valid"}, null]` {
		t.Fatalf("raw output = %#v, %v", page, err)
	}
}
