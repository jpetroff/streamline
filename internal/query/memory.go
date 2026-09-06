package query

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"time"

	"streamline/internal/logmodel"
)

const (
	disconnectedGrace = 60 * time.Second
	rawChunkSize      = 64 << 10
)

type MemoryService struct {
	mu        sync.Mutex
	session   Session
	compiler  Compiler
	records   []Record
	rawChunks []string
	queries   map[string]*memoryQuery
	now       func() time.Time
	grace     time.Duration
}

type memoryQuery struct {
	id          string
	predicate   Predicate
	state       State
	matches     []uint64
	snapshots   map[string]snapshotIndex
	subscribers map[*subscription]struct{}
	expiresAt   time.Time
	notifyTimer *time.Timer
	pendingType string
	canceled    bool
}

type snapshotIndex struct {
	descriptor Snapshot
	count      int
}

type subscription struct {
	once   sync.Once
	events chan Event
	close  func()
}

// Events exposes the bounded latest-state notification channel.
func (s *subscription) Events() <-chan Event { return s.events }

// Close detaches the subscriber exactly once and begins its query grace period.
func (s *subscription) Close() { s.once.Do(s.close) }

// NewMemoryService creates a session-scoped in-memory query coordinator.
func NewMemoryService(compiler Compiler) *MemoryService {
	if compiler == nil {
		compiler = MatchEmptyCompiler{}
	}
	return &MemoryService{
		session:  Session{SessionID: randomID(), GenerationID: "1", InputStatus: InputStreaming, InputKind: InputPending},
		compiler: compiler,
		queries:  make(map[string]*memoryQuery),
		now:      time.Now,
		grace:    disconnectedGrace,
	}
}

// Session returns a consistent copy of current session metadata.
func (s *MemoryService) Session(context.Context) Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session
}

// Raw returns a stable page of display-safe stdin chunks for the current generation.
func (s *MemoryService) Raw(_ context.Context, generation string, offset uint64, limit int) (RawChunkPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session.InputKind != InputRaw {
		return RawChunkPage{}, ErrRawUnavailable
	}
	if generation != s.session.GenerationID {
		return RawChunkPage{}, ErrGenerationChanged
	}
	if offset > uint64(len(s.rawChunks)) {
		return RawChunkPage{}, &APIError{Code: "invalid_offset", Message: "offset exceeds the available raw output"}
	}
	end := int(offset) + limit
	if end > len(s.rawChunks) {
		end = len(s.rawChunks)
	}
	chunks := append([]string(nil), s.rawChunks[int(offset):end]...)
	return RawChunkPage{
		GenerationID: s.session.GenerationID,
		Offset:       strconv.FormatUint(offset, 10),
		TotalChunks:  strconv.Itoa(len(s.rawChunks)),
		Chunks:       chunks,
	}, nil
}

// Create compiles a filter, captures the committed boundary, and starts an atomic initial scan.
func (s *MemoryService) Create(_ context.Context, request CreateRequest) (State, error) {
	if request.Sort == "" {
		request.Sort = SortInput
	}
	if request.Sort != SortInput {
		return State{}, &APIError{Code: "invalid_sort", Message: "only input order is currently supported"}
	}
	predicate, err := s.compiler.Compile(request.Filter)
	if err != nil {
		return State{}, err
	}

	search, err := compileSearch(request.Search)
	if err != nil {
		return State{}, err
	}
	// General search is always the final filtering factor. Future permanent
	// predicates belong before it, so snapshots and pages index only final matches.
	predicate = allPredicates(predicate, search)

	s.mu.Lock()
	s.pruneLocked()
	id := randomID()
	boundary := len(s.records)
	q := &memoryQuery{
		id: id, predicate: predicate,
		state:     State{QueryID: id, Status: StatusBuilding, Progress: &Progress{Processed: "0", Total: strconv.Itoa(boundary)}},
		snapshots: make(map[string]snapshotIndex), subscribers: make(map[*subscription]struct{}),
	}
	s.queries[id] = q
	q.expiresAt = s.now().Add(s.grace)
	time.AfterFunc(s.grace, func() { s.mu.Lock(); defer s.mu.Unlock(); s.pruneLocked() })
	records := append([]Record(nil), s.records[:boundary]...)
	initial := cloneState(q.state)
	s.mu.Unlock()

	go s.build(q, records)
	return initial, nil
}

// build scans the captured prefix and catches up concurrent appends before publishing ready state.
func (s *MemoryService) build(q *memoryQuery, records []Record) {
	matches := make([]uint64, 0, len(records))
	for i, record := range records {
		if q.predicate(record) {
			matches = append(matches, record.ID)
		}
		if i > 0 && i%1024 == 0 {
			s.mu.Lock()
			if q.canceled {
				s.mu.Unlock()
				return
			}
			q.state.Progress.Processed = strconv.Itoa(i + 1)
			s.scheduleLocked(q, "progress")
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if q.canceled {
		return
	}
	// Append skips building queries. Catch up while holding the same lock so
	// records accepted during the initial scan cannot be missed.
	boundary := len(s.records)
	for _, record := range s.records[len(records):boundary] {
		if q.predicate(record) {
			matches = append(matches, record.ID)
		}
	}
	q.matches = matches
	q.state.Status = StatusReady
	q.state.Progress = nil
	s.publishLocked(q, uint64(boundary))
}

// Get returns a copy of the latest authoritative query state.
func (s *MemoryService) Get(_ context.Context, id string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	q, ok := s.queries[id]
	if !ok {
		return State{}, ErrNotFound
	}
	return cloneState(q.state), nil
}

// Page resolves an opaque snapshot token to a stable matching-record window.
func (s *MemoryService) Page(_ context.Context, id, token string, offset uint64, limit int) (Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	q, ok := s.queries[id]
	if !ok {
		return Page{}, ErrNotFound
	}
	snapshot, ok := q.snapshots[token]
	if !ok {
		return Page{}, ErrSnapshotGone
	}
	if offset > uint64(snapshot.count) {
		offset = uint64(snapshot.count)
	}
	end := int(offset) + limit
	if end > snapshot.count {
		end = snapshot.count
	}
	rows := make([]Row, 0, end-int(offset))
	for _, recordID := range q.matches[int(offset):end] {
		if recordID == 0 || recordID > uint64(len(s.records)) {
			continue
		}
		r := logmodel.CloneRecord(s.records[recordID-1])
		rows = append(rows, Row{
			ID: strconv.FormatUint(r.ID, 10), Timestamp: r.Timestamp, Severity: r.Severity,
			Message: r.Message, Fields: r.Fields, SourceFormat: r.SourceFormat, Diagnostics: r.Diagnostics,
		})
	}
	return Page{Snapshot: snapshot.descriptor, Offset: strconv.FormatUint(offset, 10), Rows: rows}, nil
}

// Subscribe atomically registers a bounded subscriber and returns the current state for gap-free startup.
func (s *MemoryService) Subscribe(_ context.Context, id string) (State, Session, Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	q, ok := s.queries[id]
	if !ok {
		return State{}, Session{}, nil, ErrNotFound
	}
	sub := &subscription{events: make(chan Event, 1)}
	sub.close = func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if q.canceled {
			return
		}
		delete(q.subscribers, sub)
		if len(q.subscribers) == 0 {
			q.expiresAt = s.now().Add(s.grace)
			time.AfterFunc(s.grace, func() { s.mu.Lock(); defer s.mu.Unlock(); s.pruneLocked() })
		}
	}
	q.subscribers[sub] = struct{}{}
	q.expiresAt = time.Time{}
	return cloneState(q.state), s.session, sub, nil
}

// Delete cancels a query, stops notifications, and releases its snapshots.
func (s *MemoryService) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.queries[id]
	if !ok {
		return ErrNotFound
	}
	q.canceled = true
	delete(s.queries, id)
	if q.notifyTimer != nil {
		q.notifyTimer.Stop()
	}
	for sub := range q.subscribers {
		close(sub.events)
		delete(q.subscribers, sub)
	}
	return nil
}

// Append publishes a complete input batch and extends every ready live query. It is the ingestion integration point.
func (s *MemoryService) Append(records []Record) {
	if len(records) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session.InputKind == InputPending {
		s.session.InputKind = InputRecords
	}
	appended := make([]Record, 0, len(records))
	for i := range records {
		record := logmodel.CloneRecord(records[i])
		record.ID = uint64(len(s.records) + 1)
		if record.SourceFormat == "" {
			record.SourceFormat = logmodel.FormatText
		}
		s.records = append(s.records, record)
		appended = append(appended, record)
	}
	boundary := uint64(len(s.records))
	for _, q := range s.queries {
		if q.canceled || q.state.Status != StatusReady {
			continue
		}
		for _, record := range appended {
			if q.predicate(record) {
				q.matches = append(q.matches, record.ID)
			}
		}
		s.publishLocked(q, boundary)
	}
}

// SetInputStatus updates session state and immediately tells connected viewers.
func (s *MemoryService) SetInputStatus(status InputStatus, inputErr *APIError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session.InputStatus, s.session.Error = status, inputErr
	for _, q := range s.queries {
		s.pushLocked(q, "input")
	}
}

// SetRawOutput atomically publishes terminal display-safe stdin text and status.
func (s *MemoryService) SetRawOutput(output string, status InputStatus, inputErr *APIError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rawChunks = splitRawChunks(output)
	s.session.InputKind = InputRaw
	s.session.InputStatus = status
	s.session.Error = inputErr
	for _, q := range s.queries {
		s.pushLocked(q, "input")
	}
}

// publishLocked records a new immutable logical boundary; the caller must hold mu.
func (s *MemoryService) publishLocked(q *memoryQuery, boundary uint64) {
	revision := uint64(len(q.snapshots) + 1)
	token := fmt.Sprintf("%s.%d.%d", q.id, revision, len(q.matches))
	d := Snapshot{
		SessionID: s.session.SessionID, GenerationID: s.session.GenerationID, QueryID: q.id,
		Revision: strconv.FormatUint(revision, 10), ProcessedThrough: strconv.FormatUint(boundary, 10),
		MatchedCount: strconv.Itoa(len(q.matches)), SnapshotToken: token,
	}
	q.snapshots[token] = snapshotIndex{descriptor: d, count: len(q.matches)}
	q.state.Snapshot = &d
	s.scheduleLocked(q, "snapshot")
}

// scheduleLocked coalesces ordinary changes into at most one notification per 100 ms.
func (s *MemoryService) scheduleLocked(q *memoryQuery, eventType string) {
	q.pendingType = eventType
	if q.notifyTimer != nil {
		return
	}
	q.notifyTimer = time.AfterFunc(100*time.Millisecond, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if q.canceled {
			return
		}
		q.notifyTimer = nil
		eventType := q.pendingType
		q.pendingType = ""
		s.pushLocked(q, eventType)
	})
}

// pushLocked replaces a slow subscriber's queued event with the newest state.
func (s *MemoryService) pushLocked(q *memoryQuery, eventType string) {
	event := Event{Type: eventType, State: cloneState(q.state), Session: s.session}
	for sub := range q.subscribers {
		select {
		case sub.events <- event:
		default:
			select {
			case <-sub.events:
			default:
			}
			select {
			case sub.events <- event:
			default:
			}
		}
	}
}

// pruneLocked releases disconnected queries whose grace period has elapsed.
func (s *MemoryService) pruneLocked() {
	now := s.now()
	for id, q := range s.queries {
		if !q.expiresAt.IsZero() && !now.Before(q.expiresAt) {
			q.canceled = true
			delete(s.queries, id)
			if q.notifyTimer != nil {
				q.notifyTimer.Stop()
			}
			for sub := range q.subscribers {
				close(sub.events)
				delete(q.subscribers, sub)
			}
		}
	}
}

// cloneState prevents callers and subscribers from observing later pointer mutations.
func cloneState(state State) State {
	copy := state
	if state.Progress != nil {
		p := *state.Progress
		copy.Progress = &p
	}
	if state.Snapshot != nil {
		s := *state.Snapshot
		copy.Snapshot = &s
	}
	if state.Error != nil {
		e := *state.Error
		e.LineErrors = append([]LineError(nil), e.LineErrors...)
		copy.Error = &e
	}
	return copy
}

// randomID returns an unpredictable session or query identifier.
func randomID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}

// splitRawChunks creates UTF-8-safe transport chunks without inserting separators.
func splitRawChunks(output string) []string {
	if output == "" {
		return nil
	}
	data := []byte(output)
	chunks := make([]string, 0, (len(data)+rawChunkSize-1)/rawChunkSize)
	for start := 0; start < len(data); {
		end := start + rawChunkSize
		if end >= len(data) {
			end = len(data)
		} else {
			for end > start && data[end]&0xc0 == 0x80 {
				end--
			}
		}
		chunks = append(chunks, string(data[start:end]))
		start = end
	}
	return chunks
}
