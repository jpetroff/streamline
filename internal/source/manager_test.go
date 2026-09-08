//go:build linux || darwin

package source

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"streamline/internal/query"
)

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func create(t *testing.T, m *Manager, command, mode string) Info {
	t.Helper()
	info, err := m.Create(CreateRequest{Command: command, Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return info
}
func completed(t *testing.T, m *Manager, id string) Info {
	t.Helper()
	var result Info
	eventually(t, func() bool {
		for _, info := range m.List() {
			if info.ID == id {
				result = info
				return info.State == "exited" || info.State == "failed" || info.State == "stopped"
			}
		}
		return false
	})
	return result
}
func rows(t *testing.T, m *Manager, id string) []query.Row {
	t.Helper()
	q, err := m.Queries(id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := q.Create(context.Background(), query.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		state, err = q.Get(context.Background(), state.QueryID)
		return err == nil && state.Status == query.StatusReady
	})
	page, err := q.Page(context.Background(), state.QueryID, state.Snapshot.SnapshotToken, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return page.Rows
}

func TestCommandsFiniteAndIsolated(t *testing.T) {
	t.Parallel()
	m := New()
	defer m.Close()
	a := create(t, m, `word='hello world'; printf '%s\n' "$word" | cat; printf 'diagnostic\n' >&2; printf 'partial'`, "text")
	b := create(t, m, `printf '{"message":"separate"}\n'`, "auto")
	if info := completed(t, m, a.ID); info.State != "exited" || *info.ExitCode != 0 || info.Session.InputStatus != query.InputEOF {
		t.Fatalf("completion = %+v", info)
	}
	completed(t, m, b.ID)
	got := rows(t, m, a.ID)
	if len(got) != 3 || got[0].Message != "hello world" || got[1].Message != "diagnostic" || got[2].Message != "partial" {
		t.Fatalf("rows = %#v", got)
	}
	if got := rows(t, m, b.ID); len(got) != 1 || got[0].Message != "separate" {
		t.Fatalf("other source = %#v", got)
	}
	if got := rows(t, m, "stdin"); len(got) != 0 {
		t.Fatalf("stdin contaminated: %#v", got)
	}
}

func TestCommandStreamsBeforeExitAndStopRetainsOutput(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"text", "auto"} {
		t.Run(mode, func(t *testing.T) {
			m := New()
			defer m.Close()
			command := `printf 'live\n'; sleep 30`
			if mode == "auto" {
				command = `printf '{"message":"live"}\n'; sleep 30`
			}
			info := create(t, m, command, mode)
			q, _ := m.Queries(info.ID)
			eventually(t, func() bool { return q.Session(context.Background()).InputKind == query.InputRecords })
			if got := rows(t, m, info.ID); len(got) != 1 || got[0].Message != "live" {
				t.Fatalf("live rows = %#v", got)
			}
			if err := m.Stop(info.ID); err != nil {
				t.Fatal(err)
			}
			if err := m.Stop(info.ID); err != nil {
				t.Fatal(err)
			}
			if result := completed(t, m, info.ID); result.State != "stopped" || result.Error != nil {
				t.Fatalf("stop = %+v", result)
			}
			if len(rows(t, m, info.ID)) != 1 {
				t.Fatal("stop discarded logs")
			}
		})
	}
}

func TestRawFailureAndEmptyOutput(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		command, mode, state, raw string
		code                      int
	}{
		{`printf 'plain report'; exit 7`, "auto", "failed", "plain report", 7},
		{`printf 'plain report'`, "auto", "exited", "plain report", 0},
		{`true`, "auto", "exited", "", 0},
		{`true`, "text", "exited", "", 0},
		{`streamline_command_that_does_not_exist`, "auto", "failed", "", 127},
	} {
		t.Run(test.command+test.mode, func(t *testing.T) {
			m := New()
			defer m.Close()
			info := create(t, m, test.command, test.mode)
			result := completed(t, m, info.ID)
			if result.State != test.state || *result.ExitCode != test.code || result.Session.InputKind == query.InputPending {
				t.Fatalf("result = %+v", result)
			}
			if test.code != 0 && (result.Error == nil || result.Session.InputStatus != query.InputError) {
				t.Fatal("failure not exposed")
			}
			if test.raw != "" {
				q, _ := m.Queries(info.ID)
				raw, err := q.Raw(context.Background(), result.Session.GenerationID, 0, 4)
				if err != nil || strings.Join(raw.Chunks, "") != test.raw {
					t.Fatalf("raw = %+v %v", raw, err)
				}
			}
		})
	}
}

func TestLargeOutputDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	m := New()
	defer m.Close()
	info := create(t, m, `i=0; while [ "$i" -lt 10000 ]; do printf 'line %s abcdefghijklmnopqrstuvwxyz\n' "$i"; i=$((i+1)); done`, "text")
	completed(t, m, info.ID)
	q, _ := m.Queries(info.ID)
	state, _ := q.Create(context.Background(), query.CreateRequest{})
	eventually(t, func() bool {
		state, _ = q.Get(context.Background(), state.QueryID)
		return state.Snapshot != nil && state.Snapshot.MatchedCount == "10000"
	})
}

func TestCloseStopsTermResistantGroup(t *testing.T) {
	t.Parallel()
	m := New()
	info := create(t, m, `trap '' TERM; printf 'ready\n'; while :; do sleep 30; done`, "text")
	q, _ := m.Queries(info.ID)
	eventually(t, func() bool { return q.Session(context.Background()).InputKind == query.InputRecords })
	start := time.Now()
	m.Close()
	if time.Since(start) > 4*time.Second {
		t.Fatal("shutdown exceeded bound")
	}
	if _, err := m.Create(CreateRequest{Command: "true"}); err == nil {
		t.Fatal("creation allowed after shutdown")
	}
	m.Close()
}

func TestInheritedDescriptorIsBounded(t *testing.T) {
	t.Parallel()
	m := New()
	defer m.Close()
	info := create(t, m, `sleep 30 & printf 'before exit\n'`, "text")
	completed(t, m, info.ID)
	if got := rows(t, m, info.ID); len(got) != 1 || got[0].Message != "before exit" {
		t.Fatalf("rows = %#v", got)
	}
}

func TestRemoveReleasesQueriesAndEvents(t *testing.T) {
	t.Parallel()
	m := New()
	defer m.Close()
	info := create(t, m, `printf 'live\n'; sleep 30`, "text")
	q, _ := m.Queries(info.ID)
	created, _ := q.Create(context.Background(), query.CreateRequest{})
	_, _, subscription, err := q.Subscribe(context.Background(), created.QueryID)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	if err := m.Remove(info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Queries(info.ID); err == nil {
		t.Fatal("removed source still available")
	}
	if _, err := q.Create(context.Background(), query.CreateRequest{}); err == nil {
		t.Fatal("closed service accepted a query")
	}
	for range subscription.Events() {
	}
	if len(m.List()) != 1 {
		t.Fatal("source retained")
	}
}

func TestValidationStdinAndLifecycleSubscription(t *testing.T) {
	t.Parallel()
	m := New()
	defer m.Close()
	for _, request := range []CreateRequest{{Command: " "}, {Command: "a\x00b"}, {Command: "true", Mode: "invalid"}} {
		if _, err := m.Create(request); err == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
	if m.Stop("stdin") == nil || m.Remove("stdin") == nil {
		t.Fatal("stdin mutation allowed")
	}
	events, unsubscribe := m.Subscribe()
	defer unsubscribe()
	<-events
	reader, writer := io.Pipe()
	m.StartStdin(reader)
	fmt.Fprintln(writer, `{"message":"stdin"}`)
	writer.Close()
	eventually(t, func() bool { return m.List()[0].State == "exited" })
	select {
	case <-events:
	default:
		t.Fatal("missing lifecycle notification")
	}
	if len(rows(t, m, "stdin")) != 1 {
		t.Fatal("stdin not ingested")
	}
}

func TestShutdownInterruptsOpenOSStdinPipe(t *testing.T) {
	t.Parallel()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	input, err := interruptibleFile(reader)
	if err != nil {
		t.Fatal(err)
	}
	m := New()
	m.StartStdin(input)
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked on open stdin")
	}
}

func TestFailedProcessStartupIsRetained(t *testing.T) {
	t.Parallel()
	m := New()
	defer m.Close()
	m.shellPath = t.TempDir() + "/missing-shell"
	info := create(t, m, "true", "auto")
	result := completed(t, m, info.ID)
	if result.State != "failed" || result.Error == nil || result.Error.Code != "command_start_failed" || result.Session.InputStatus != query.InputError {
		t.Fatalf("startup = %+v", result)
	}
	if err := m.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(info.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStopTerminatesBackgroundWritersInTheProcessGroup(t *testing.T) {
	t.Parallel()
	m := New()
	defer m.Close()
	marker := t.TempDir() + "/child-writes"
	quoted := "'" + strings.ReplaceAll(marker, "'", "'\"'\"'") + "'"
	info := create(t, m, `trap '' TERM; while :; do printf x >> `+quoted+`; sleep 0.05; done & printf 'ready\n'; wait`, "text")
	eventually(t, func() bool { stat, err := os.Stat(marker); return err == nil && stat.Size() > 0 })
	if err := m.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	completed(t, m, info.ID)
	before, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	after, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() {
		t.Fatal("background child kept writing after Stop completed")
	}
}
