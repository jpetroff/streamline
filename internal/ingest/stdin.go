// Package ingest connects input producers to the parser and query service.
package ingest

import (
	"context"
	"io"
	"time"

	"streamline/internal/parse"
	"streamline/internal/query"
)

const (
	maxBatchRecords = 512
	maxBatchDelay   = 100 * time.Millisecond
)

// Sink accepts committed parsed batches and terminal input state.
type Sink interface {
	Append([]query.Record)
	SetInputStatus(query.InputStatus, *query.APIError)
	SetRawOutput(string, query.InputStatus, *query.APIError)
}

type outcome struct {
	result *parse.LoadResult
	err    error
}

// Completion holds terminal input data after all parsed batches have been flushed.
type Completion struct {
	Raw *string
	Err error
}

// Publish finalizes a source, optionally replacing its read error with a process error.
func (c Completion) Publish(sink Sink, terminalErr *query.APIError) {
	status := query.InputEOF
	if terminalErr == nil && c.Err != nil {
		terminalErr = &query.APIError{Code: "input_read_error", Message: c.Err.Error()}
	}
	if terminalErr != nil {
		status = query.InputError
	}
	if c.Raw != nil {
		sink.SetRawOutput(*c.Raw, status, terminalErr)
	} else {
		sink.SetInputStatus(status, terminalErr)
	}
}

// Run progressively parses reader until EOF, error, or context cancellation.
func Run(ctx context.Context, reader io.Reader, engine *parse.Engine, sink Sink) {
	completed := Capture(ctx, reader, engine, sink)
	if ctx.Err() == nil {
		completed.Publish(sink, nil)
	}
}

// Capture flushes parsed input without publishing terminal status. Owners of
// blocking readers must close them before joining this function on shutdown.
func Capture(ctx context.Context, reader io.Reader, engine *parse.Engine, sink Sink) Completion {
	batches := make(chan []parse.CapturedRecord)
	done := make(chan outcome, 1)
	go func() {
		result, err := engine.Stream(reader, func(records []parse.CapturedRecord) {
			select {
			case batches <- records:
			case <-ctx.Done():
			}
		})
		done <- outcome{result: result, err: err}
	}()

	ticker := time.NewTicker(maxBatchDelay)
	defer ticker.Stop()
	pending := make([]query.Record, 0, maxBatchRecords)

	flushFull := func() {
		for len(pending) >= maxBatchRecords {
			sink.Append(pending[:maxBatchRecords])
			pending = pending[maxBatchRecords:]
		}
	}
	flushAll := func() {
		flushFull()
		if len(pending) > 0 {
			sink.Append(pending)
			pending = nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			flushAll()
			return Completion{Err: ctx.Err()}
		case records := <-batches:
			for _, record := range records {
				pending = append(pending, record.Entry)
			}
			flushFull()
		case <-ticker.C:
			flushAll()
		case completed := <-done:
			flushAll()
			completion := Completion{Err: completed.err}
			if completed.result != nil && completed.result.Kind == parse.ResultRaw && completed.result.Raw != nil {
				completion.Raw = &completed.result.Raw.Text
			}
			return completion
		}
	}
}
