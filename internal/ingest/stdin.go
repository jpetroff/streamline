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

// Run progressively parses reader until EOF, error, or context cancellation.
func Run(ctx context.Context, reader io.Reader, engine *parse.Engine, sink Sink) {
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
			return
		case records := <-batches:
			for _, record := range records {
				pending = append(pending, record.Entry)
			}
			flushFull()
		case <-ticker.C:
			flushAll()
		case completed := <-done:
			flushAll()
			status := query.InputEOF
			var inputErr *query.APIError
			if completed.err != nil {
				status = query.InputError
				inputErr = &query.APIError{Code: "input_read_error", Message: completed.err.Error()}
			}
			if completed.result != nil && completed.result.Kind == parse.ResultRaw && completed.result.Raw != nil {
				sink.SetRawOutput(completed.result.Raw.Text, status, inputErr)
			} else {
				sink.SetInputStatus(status, inputErr)
			}
			return
		}
	}
}
