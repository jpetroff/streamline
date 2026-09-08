// Package source owns independent log inputs and their process lifetimes.
package source

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"streamline/internal/ingest"
	"streamline/internal/parse"
	"streamline/internal/query"
)

// Info is an authoritative source descriptor. Logs remain in its query service.
type Info struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Command    string          `json:"command,omitempty"`
	Mode       string          `json:"mode"`
	State      string          `json:"state"`
	CreatedAt  time.Time       `json:"createdAt"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
	ExitCode   *int            `json:"exitCode,omitempty"`
	Error      *query.APIError `json:"error,omitempty"`
	Session    query.Session   `json:"session"`
}

type CreateRequest struct {
	Command string `json:"command"`
	Mode    string `json:"mode"`
}

type input struct {
	info          Info
	queries       *query.MemoryService
	stop          chan struct{}
	done          chan struct{}
	stopRequested bool
}

// Manager keeps sources alive independently of HTTP requests and browser tabs.
type Manager struct {
	mu        sync.Mutex
	inputs    map[string]*input
	order     []string
	watchers  map[chan struct{}]struct{}
	closed    bool
	stdin     io.ReadCloser
	stdinDone chan struct{}
	closeDone chan struct{}
	shellPath string
}

func New() *Manager {
	q := query.NewMemoryService(nil)
	return &Manager{
		inputs: map[string]*input{"stdin": {info: Info{ID: "stdin", Kind: "stdin", Mode: "auto", State: "running", CreatedAt: time.Now()}, queries: q}},
		order:  []string{"stdin"}, watchers: make(map[chan struct{}]struct{}), closeDone: make(chan struct{}), shellPath: "/bin/sh",
	}
}

func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Info, 0, len(m.order))
	for _, id := range m.order {
		s := m.inputs[id]
		info := s.info
		info.Session = s.queries.Session(context.Background())
		result = append(result, info)
	}
	return result
}

func sourceError(code, message string) *query.APIError {
	return &query.APIError{Code: code, Message: message}
}

func (m *Manager) Queries(id string) (*query.MemoryService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.inputs[id]
	if s == nil || m.closed {
		return nil, sourceError("source_not_found", "source does not exist")
	}
	return s.queries, nil
}

// Subscribe announces that callers should reread the complete source list.
func (m *Manager) Subscribe() (<-chan struct{}, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan struct{}, 1)
	if m.closed {
		close(ch)
	} else {
		m.watchers[ch] = struct{}{}
		ch <- struct{}{}
	}
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if _, ok := m.watchers[ch]; ok {
				delete(m.watchers, ch)
				close(ch)
			}
		})
	}
}

func (m *Manager) changedLocked() {
	for ch := range m.watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// StartStdin transfers ownership of reader until Manager.Close.
func (m *Manager) StartStdin(reader io.ReadCloser) {
	m.mu.Lock()
	if m.closed || m.stdinDone != nil {
		m.mu.Unlock()
		return
	}
	m.stdin, m.stdinDone = reader, make(chan struct{})
	s, done := m.inputs["stdin"], m.stdinDone
	m.mu.Unlock()
	go func() {
		defer close(done)
		completion := ingest.Capture(context.Background(), reader, parse.NewEngine(parse.Options{}), &sourceSink{manager: m, input: s})
		completion.Publish(s.queries, nil)
		m.mu.Lock()
		defer m.mu.Unlock()
		s.info.State = "exited"
		if completion.Err != nil {
			s.info.State = "failed"
			s.info.Error = sourceError("input_read_error", completion.Err.Error())
		}
		now := time.Now()
		s.info.FinishedAt = &now
		m.changedLocked()
	}()
}

func (m *Manager) Create(request CreateRequest) (Info, error) {
	if strings.TrimSpace(request.Command) == "" || strings.ContainsRune(request.Command, 0) {
		return Info{}, sourceError("invalid_command", "command must be nonempty and contain no NUL bytes")
	}
	if request.Mode == "" {
		request.Mode = "auto"
	}
	if request.Mode != "auto" && request.Mode != "text" {
		return Info{}, sourceError("invalid_mode", "mode must be auto or text")
	}
	if !supported {
		return Info{}, sourceError("unsupported_platform", "command sources require Linux or macOS")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Info{}, sourceError("source_manager_closed", "application is shutting down")
	}
	queries := query.NewMemoryService(nil)
	id := "command-" + queries.Session(context.Background()).SessionID
	s := &input{info: Info{ID: id, Kind: "command", Command: request.Command, Mode: request.Mode, State: "starting", CreatedAt: time.Now()}, queries: queries, stop: make(chan struct{}), done: make(chan struct{})}
	m.inputs[id] = s
	m.order = append(m.order, id)
	m.changedLocked()
	info := s.info
	info.Session = s.queries.Session(context.Background())
	go m.run(s)
	return info, nil
}

func (m *Manager) requestStopLocked(s *input) {
	if s.stopRequested || s.info.State == "exited" || s.info.State == "failed" || s.info.State == "stopped" {
		return
	}
	s.stopRequested = true
	s.info.State = "stopping"
	close(s.stop)
	m.changedLocked()
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.inputs[id]
	if s == nil {
		return sourceError("source_not_found", "source does not exist")
	}
	if s.info.Kind == "stdin" {
		return sourceError("immutable_source", "stdin cannot be stopped or removed")
	}
	m.requestStopLocked(s)
	return nil
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	s := m.inputs[id]
	if s == nil {
		m.mu.Unlock()
		return sourceError("source_not_found", "source does not exist")
	}
	if s.info.Kind == "stdin" {
		m.mu.Unlock()
		return sourceError("immutable_source", "stdin cannot be stopped or removed")
	}
	m.requestStopLocked(s)
	m.mu.Unlock()
	<-s.done
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inputs[id] != s {
		return nil
	}
	delete(m.inputs, id)
	for i, current := range m.order {
		if current == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	s.queries.Close()
	m.changedLocked()
	return nil
}

// Close stops all command groups concurrently, joins producers, and releases data.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		<-m.closeDone
		return
	}
	m.closed = true
	inputs := make([]*input, 0, len(m.inputs))
	for _, s := range m.inputs {
		inputs = append(inputs, s)
		if s.stop != nil {
			m.requestStopLocked(s)
		}
	}
	reader, stdinDone := m.stdin, m.stdinDone
	m.mu.Unlock()
	if reader != nil {
		_ = reader.Close()
	}
	for _, s := range inputs {
		if s.done != nil {
			<-s.done
		}
	}
	if stdinDone != nil {
		<-stdinDone
	}
	for _, s := range inputs {
		s.queries.Close()
	}
	m.mu.Lock()
	for ch := range m.watchers {
		close(ch)
		delete(m.watchers, ch)
	}
	m.mu.Unlock()
	close(m.closeDone)
}

// sourceSink announces classification changes, not every batch of log rows.
type sourceSink struct {
	manager    *Manager
	input      *input
	classified bool
}

func (s *sourceSink) Append(records []query.Record) {
	s.input.queries.Append(records)
	if len(records) > 0 && !s.classified {
		s.classified = true
		s.manager.mu.Lock()
		s.manager.changedLocked()
		s.manager.mu.Unlock()
	}
}
func (s *sourceSink) SetInputStatus(status query.InputStatus, err *query.APIError) {
	s.input.queries.SetInputStatus(status, err)
}
func (s *sourceSink) SetRawOutput(raw string, status query.InputStatus, err *query.APIError) {
	s.input.queries.SetRawOutput(raw, status, err)
}

func (m *Manager) finish(s *input, completion ingest.Completion, exitCode *int, runErr *query.APIError) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := "exited"
	if runErr != nil || completion.Err != nil {
		state = "failed"
	}
	if s.stopRequested {
		state = "stopped"
		runErr = nil
		completion.Err = nil
	}
	if runErr == nil && completion.Err != nil {
		runErr = sourceError("input_read_error", completion.Err.Error())
	}
	completion.Publish(s.queries, runErr)
	s.info.State, s.info.ExitCode, s.info.Error = state, exitCode, runErr
	now := time.Now()
	s.info.FinishedAt = &now
	m.changedLocked()
}
