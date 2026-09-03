// Package query owns query lifecycle and immutable result snapshots.
package query

import (
	"context"
	"errors"
)

const (
	DefaultPageSize = 200
	MaxPageSize     = 1000
)

type InputStatus string

const (
	InputStreaming InputStatus = "streaming"
	InputEOF       InputStatus = "eof"
	InputError     InputStatus = "error"
)

type Status string

const (
	StatusBuilding Status = "building"
	StatusReady    Status = "ready"
	StatusFailed   Status = "failed"
)

type Sort string

const (
	SortInput Sort = "input"
)

type Session struct {
	SessionID    string      `json:"sessionId"`
	GenerationID string      `json:"generationId"`
	InputStatus  InputStatus `json:"inputStatus"`
	Error        *APIError   `json:"error,omitempty"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error lets APIError cross service boundaries as a standard Go error.
func (e *APIError) Error() string { return e.Message }

var (
	ErrNotFound       = &APIError{Code: "query_not_found", Message: "query does not exist or has expired"}
	ErrSnapshotGone   = &APIError{Code: "snapshot_invalid", Message: "snapshot is not available"}
	ErrQueryEngine    = &APIError{Code: "query_engine_unavailable", Message: "filtering is not available until the query engine is connected"}
	ErrInvalidRequest = &APIError{Code: "invalid_request", Message: "request is invalid"}
)

type Record struct {
	ID        uint64            `json:"-"`
	Timestamp string            `json:"timestamp,omitempty"`
	Severity  string            `json:"severity,omitempty"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type Row struct {
	ID        string            `json:"id"`
	Timestamp string            `json:"timestamp,omitempty"`
	Severity  string            `json:"severity,omitempty"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type Snapshot struct {
	SessionID        string `json:"sessionId"`
	GenerationID     string `json:"generationId"`
	QueryID          string `json:"queryId"`
	Revision         string `json:"revision"`
	ProcessedThrough string `json:"processedThrough"`
	MatchedCount     string `json:"matchedCount"`
	SnapshotToken    string `json:"snapshotToken"`
}

type Progress struct {
	Processed string `json:"processed"`
	Total     string `json:"total"`
}

type State struct {
	QueryID  string    `json:"queryId"`
	Status   Status    `json:"status"`
	Progress *Progress `json:"progress,omitempty"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Error    *APIError `json:"error,omitempty"`
}

type Page struct {
	Snapshot Snapshot `json:"snapshot"`
	Offset   string   `json:"offset"`
	Rows     []Row    `json:"rows"`
}

type CreateRequest struct {
	Filter string `json:"filter"`
	Sort   Sort   `json:"sort"`
}

type Event struct {
	Type    string  `json:"type"`
	State   State   `json:"state"`
	Session Session `json:"session"`
}

type Subscription interface {
	Events() <-chan Event
	Close()
}

type Service interface {
	Session(context.Context) Session
	Create(context.Context, CreateRequest) (State, error)
	Get(context.Context, string) (State, error)
	Page(context.Context, string, string, uint64, int) (Page, error)
	Subscribe(context.Context, string) (State, Session, Subscription, error)
	Delete(context.Context, string) error
}

type Predicate func(Record) bool

type Compiler interface {
	Compile(string) (Predicate, error)
}

type CompilerFunc func(string) (Predicate, error)

// Compile adapts a function into the query compiler interface.
func (f CompilerFunc) Compile(expression string) (Predicate, error) { return f(expression) }

type MatchEmptyCompiler struct{}

// Compile accepts the production scaffold's unfiltered query and rejects filters until the shared engine is connected.
func (MatchEmptyCompiler) Compile(expression string) (Predicate, error) {
	if expression != "" {
		return nil, ErrQueryEngine
	}
	return func(Record) bool { return true }, nil
}

// AsAPIError preserves public domain errors and masks unexpected internal failures.
func AsAPIError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return &APIError{Code: "internal_error", Message: "an internal error occurred"}
}
