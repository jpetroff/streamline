package query

import (
	"context"
	"testing"
	"time"
)

type blockingCompiler struct{ entered, release chan struct{} }

func (c blockingCompiler) Compile(string) (Predicate, error) {
	return func(Record) bool {
		select {
		case c.entered <- struct{}{}:
		default:
		}
		<-c.release
		return true
	}, nil
}

func readyState(t *testing.T, service *MemoryService, id string) State {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, err := service.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status == StatusReady {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("query did not become ready")
	return State{}
}

func TestBuildCatchesRecordsAppendedDuringInitialScan(t *testing.T) {
	compiler := blockingCompiler{entered: make(chan struct{}, 1), release: make(chan struct{})}
	service := NewMemoryService(compiler)
	service.Append([]Record{{Message: "first"}})
	state, err := service.Create(context.Background(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	<-compiler.entered
	service.Append([]Record{{Message: "second"}})
	close(compiler.release)
	ready := readyState(t, service, state.QueryID)
	if ready.Snapshot.ProcessedThrough != "2" {
		t.Fatalf("processed boundary = %s, want 2", ready.Snapshot.ProcessedThrough)
	}
	if ready.Snapshot.MatchedCount != "2" {
		t.Fatalf("matched count = %s, want 2", ready.Snapshot.MatchedCount)
	}
}

func TestSnapshotsKeepStablePaginationWhileLiveQueryExtends(t *testing.T) {
	service := NewMemoryService(nil)
	service.Append([]Record{{Message: "first"}})
	created, err := service.Create(context.Background(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	first := readyState(t, service, created.QueryID).Snapshot
	service.Append([]Record{{Message: "second"}})
	latest, err := service.Get(context.Background(), created.QueryID)
	if err != nil {
		t.Fatal(err)
	}
	oldPage, err := service.Page(context.Background(), created.QueryID, first.SnapshotToken, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	newPage, err := service.Page(context.Background(), created.QueryID, latest.Snapshot.SnapshotToken, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldPage.Rows) != 1 || len(newPage.Rows) != 2 {
		t.Fatalf("page sizes = %d and %d", len(oldPage.Rows), len(newPage.Rows))
	}
}

func TestSlowSubscriberReceivesOnlyLatestCoalescedState(t *testing.T) {
	service := NewMemoryService(nil)
	created, err := service.Create(context.Background(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	readyState(t, service, created.QueryID)
	_, _, subscription, err := service.Subscribe(context.Background(), created.QueryID)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	// Drain the delayed initial publication, if it reaches this new subscriber.
	time.Sleep(120 * time.Millisecond)
	select {
	case <-subscription.Events():
	default:
	}
	for i := 0; i < 20; i++ {
		service.Append([]Record{{Message: "line"}})
	}
	time.Sleep(120 * time.Millisecond)
	select {
	case event := <-subscription.Events():
		if event.State.Snapshot.MatchedCount != "20" {
			t.Fatalf("count = %s", event.State.Snapshot.MatchedCount)
		}
	default:
		t.Fatal("subscriber did not receive coalesced state")
	}
	select {
	case <-subscription.Events():
		t.Fatal("slow subscriber received more than latest state")
	default:
	}
}

func TestCompilerOwnsFilterExpressionSemantics(t *testing.T) {
	service := NewMemoryService(CompilerFunc(func(expression string) (Predicate, error) {
		if expression != "errors" {
			t.Fatalf("expression = %q", expression)
		}
		return func(record Record) bool { return record.Severity == "error" }, nil
	}))
	service.Append([]Record{{Severity: "info", Message: "ok"}, {Severity: "error", Message: "failed"}})
	created, err := service.Create(context.Background(), CreateRequest{Filter: "errors"})
	if err != nil {
		t.Fatal(err)
	}
	ready := readyState(t, service, created.QueryID)
	if ready.Snapshot.MatchedCount != "1" {
		t.Fatalf("count = %s", ready.Snapshot.MatchedCount)
	}
}

func TestDisconnectedQueryExpiresAfterGracePeriod(t *testing.T) {
	service := NewMemoryService(nil)
	service.grace = 10 * time.Millisecond
	created, err := service.Create(context.Background(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	readyState(t, service, created.QueryID)
	_, _, subscription, err := service.Subscribe(context.Background(), created.QueryID)
	if err != nil {
		t.Fatal(err)
	}
	subscription.Close()
	time.Sleep(30 * time.Millisecond)
	if _, err := service.Get(context.Background(), created.QueryID); err != ErrNotFound {
		t.Fatalf("error = %v", err)
	}
}
