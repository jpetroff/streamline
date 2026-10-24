package ingest

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"streamline/internal/logmodel"
	"streamline/internal/parse"
	"streamline/internal/query"
)

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not reached")
}

func TestRunPublishesRecognizedRecordsBeforeEOF(t *testing.T) {
	service := query.NewMemoryService(nil)
	created, err := service.Create(context.Background(), query.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	done := make(chan struct{})
	go func() {
		Run(context.Background(), reader, parse.NewEngine(parse.Options{}), service)
		close(done)
	}()

	if _, err := writer.Write([]byte("{\"message\":\"live\"}\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		state, err := service.Get(context.Background(), created.QueryID)
		return err == nil && state.Snapshot != nil && state.Snapshot.MatchedCount == "1"
	})
	if session := service.Session(context.Background()); session.InputKind != query.InputRecords || session.InputStatus != query.InputStreaming {
		t.Fatalf("streaming session = %#v", session)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	<-done
	if session := service.Session(context.Background()); session.InputStatus != query.InputEOF {
		t.Fatalf("terminal session = %#v", session)
	}
}

func TestRunPublishesRawOnlyAfterEOF(t *testing.T) {
	service := query.NewMemoryService(nil)
	reader, writer := io.Pipe()
	done := make(chan struct{})
	go func() {
		Run(context.Background(), reader, parse.NewEngine(parse.Options{}), service)
		close(done)
	}()

	if _, err := writer.Write([]byte("plain report\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * maxBatchDelay)
	if session := service.Session(context.Background()); session.InputKind != query.InputPending {
		t.Fatalf("raw input classified before EOF: %#v", session)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	<-done
	session := service.Session(context.Background())
	if session.InputKind != query.InputRaw || session.InputStatus != query.InputEOF {
		t.Fatalf("raw session = %#v", session)
	}
	page, err := service.Raw(context.Background(), session.GenerationID, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(page.Chunks, "") != "plain report\n" {
		t.Fatalf("raw output = %#v", page)
	}
}

func TestRunPublishesLogfmtBeforeEOF(t *testing.T) {
	service := query.NewMemoryService(nil)
	created, err := service.Create(context.Background(), query.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	t.Cleanup(func() { reader.Close(); writer.Close() })
	done := make(chan struct{})
	go func() { Run(context.Background(), reader, parse.NewEngine(parse.Options{}), service); close(done) }()
	if _, err := io.WriteString(writer, "preamble\n"+`time="2026-01-03T18:07:22+02:00" level=info msg=live`+"\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		state, err := service.Get(context.Background(), created.QueryID)
		return err == nil && state.Snapshot != nil && state.Snapshot.MatchedCount == "2"
	})
	if session := service.Session(context.Background()); session.InputKind != query.InputRecords || session.InputStatus != query.InputStreaming {
		t.Fatalf("session = %#v", session)
	}
	state, err := service.Get(context.Background(), created.QueryID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.Page(context.Background(), created.QueryID, state.Snapshot.SnapshotToken, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 2 || page.Rows[0].Message != "preamble" || page.Rows[1].SourceFormat != logmodel.FormatLogfmt || page.Rows[1].Message != "live" || page.Rows[1].Timestamp != "2026-01-03T16:07:22Z" {
		t.Fatalf("rows = %#v", page.Rows)
	}
	writer.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ingestion did not finish")
	}
}

func TestJSONDocumentPublicationWaitsForValidation(t *testing.T) {
	for _, suffix := range []string{"", " trailing", ", null]"} {
		t.Run(suffix, func(t *testing.T) {
			service := query.NewMemoryService(nil)
			t.Cleanup(service.Close)
			reader, writer := io.Pipe()
			t.Cleanup(func() { reader.Close(); writer.Close() })
			done := make(chan struct{})
			go func() {
				Run(context.Background(), reader, parse.NewEngine(parse.Options{Mode: parse.ModeJSONDocument}), service)
				close(done)
			}()
			prefix := `[{"message":"ready"}]`
			if suffix == ", null]" {
				prefix = `[{"message":"ready"}`
			}
			if _, err := io.WriteString(writer, prefix); err != nil {
				t.Fatal(err)
			}
			time.Sleep(2 * maxBatchDelay)
			if session := service.Session(context.Background()); session.InputKind != query.InputPending || session.InputStatus != query.InputStreaming {
				t.Fatalf("published before EOF: %#v", session)
			}
			if _, err := io.WriteString(writer, suffix); err != nil {
				t.Fatal(err)
			}
			writer.Close()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("capture did not finish")
			}
			session := service.Session(context.Background())
			want := query.InputRecords
			if suffix != "" {
				want = query.InputRaw
			}
			if session.InputKind != want || session.InputStatus != query.InputEOF {
				t.Fatalf("terminal session = %#v", session)
			}
			if want == query.InputRaw {
				page, err := service.Raw(context.Background(), session.GenerationID, 0, 4)
				if err != nil || strings.Join(page.Chunks, "") != prefix+suffix {
					t.Fatalf("raw output = %#v, %v", page, err)
				}
			}
		})
	}
	service := query.NewMemoryService(nil)
	t.Cleanup(service.Close)
	Run(context.Background(), strings.NewReader("[]"), parse.NewEngine(parse.Options{Mode: parse.ModeJSONDocument}), service)
	if session := service.Session(context.Background()); session.InputKind != query.InputRecords || session.InputStatus != query.InputEOF {
		t.Fatalf("empty document = %#v", session)
	}
}
