package ingest

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

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
