package query

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInputKindTransitionsAndRawChunkPaging(t *testing.T) {
	records := NewMemoryService(nil)
	if session := records.Session(context.Background()); session.InputKind != InputPending {
		t.Fatalf("initial input kind = %q", session.InputKind)
	}
	records.Append([]Record{{Message: "recognized"}})
	if session := records.Session(context.Background()); session.InputKind != InputRecords {
		t.Fatalf("record input kind = %q", session.InputKind)
	}

	raw := NewMemoryService(nil)
	if _, err := raw.Raw(context.Background(), "1", 0, 1); !errors.Is(err, ErrRawUnavailable) {
		t.Fatalf("raw before publication error = %v", err)
	}
	output := strings.Repeat("a", rawChunkSize-1) + "€tail"
	raw.SetRawOutput(output, InputEOF, nil)
	session := raw.Session(context.Background())
	if session.InputKind != InputRaw || session.InputStatus != InputEOF {
		t.Fatalf("raw session = %#v", session)
	}

	first, err := raw.Raw(context.Background(), session.GenerationID, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := raw.Raw(context.Background(), session.GenerationID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalChunks != "2" || first.Offset != "0" || second.Offset != "1" {
		t.Fatalf("raw page descriptors = %#v %#v", first, second)
	}
	if strings.Join(append(first.Chunks, second.Chunks...), "") != output {
		t.Fatal("raw chunks did not reconstruct output")
	}
	if _, err := raw.Raw(context.Background(), "stale", 0, 1); !errors.Is(err, ErrGenerationChanged) {
		t.Fatalf("stale generation error = %v", err)
	}
	page, err := raw.Raw(context.Background(), session.GenerationID, 2, 1)
	if err != nil || len(page.Chunks) != 0 {
		t.Fatalf("EOF page = %#v, %v", page, err)
	}
	if _, err := raw.Raw(context.Background(), session.GenerationID, 3, 1); err == nil {
		t.Fatal("offset beyond raw output was accepted")
	}
}
