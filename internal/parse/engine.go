// Package parse loads and normalizes record-oriented log streams.
package parse

import (
	"fmt"
	"io"
	"strings"
	"time"

	"streamline/internal/logmodel"
)

// Options supplies context for timestamps that omit a timezone or year.
type Options struct {
	DefaultLocation *time.Location
	ReferenceTime   time.Time
}

// CapturedRecord connects a normalized entry to its exact source byte range.
// RawEnd is exclusive and includes the original line delimiter when present.
type CapturedRecord struct {
	Entry    logmodel.Record
	RawStart int
	RawEnd   int
}

// LoadDiagnostic describes skipped input that has no normalized record.
type LoadDiagnostic struct {
	Diagnostic logmodel.Diagnostic
	RawStart   int
	RawEnd     int
}

// ResultKind identifies the mutually exclusive parser output selected for a source.
type ResultKind string

const (
	ResultParsed ResultKind = "parsed"
	ResultRaw    ResultKind = "raw"
)

// ParsedResult contains normalized records from a recognized log stream.
type ParsedResult struct {
	Records []CapturedRecord
}

// RawResult contains display-safe text when no record identifies the input as logs.
type RawResult struct {
	Text string
}

// LoadResult owns the exact source once and selects exactly one output variant.
// CapturedRecord offsets refer to Source.
type LoadResult struct {
	Kind        ResultKind
	Source      []byte
	Parsed      *ParsedResult
	Raw         *RawResult
	Diagnostics []LoadDiagnostic
}

// Engine is an immutable, reusable parser configured with timestamp context.
type Engine struct {
	options Options
}

type parseContext struct {
	location  *time.Location
	reference time.Time
}

type frame struct {
	start      int
	contentEnd int
	end        int
}

// NewEngine builds an engine with built-in journald JSON, JSON, and text parsers.
func NewEngine(options Options) *Engine {
	return &Engine{options: options}
}

// Load reads and classifies a complete source without progressive callbacks.
func (e *Engine) Load(reader io.Reader) (*LoadResult, error) {
	return e.Stream(reader, nil)
}

// Stream reads and classifies a source, emitting completed normalized records
// after the first recognizable log record. A preamble is buffered until that
// decision and emitted before the recognizing record. If the reader fails,
// bytes read before the failure are finalized and returned with the wrapped error.
func (e *Engine) Stream(reader io.Reader, emit func([]CapturedRecord)) (*LoadResult, error) {
	result := &LoadResult{}
	context := e.context()
	records := make([]CapturedRecord, 0)
	frameStart := 0
	scanPosition := 0
	emitted := 0
	recognized := false
	buffer := make([]byte, 32<<10)

	processFrame := func(source frame) {
		cleaned := sanitizeBytes(result.Source[source.start:source.contentEnd], false)
		trimmed := strings.TrimSpace(cleaned.text)
		if trimmed == "" || isPagerArtifact(trimmed, cleaned.hadTerminal) {
			if cleaned.hadTerminal {
				result.Diagnostics = append(result.Diagnostics, LoadDiagnostic{
					Diagnostic: logmodel.Diagnostic{
						Code:    "terminal_artifact_skipped",
						Message: "terminal-only or pager display content was skipped",
					},
					RawStart: source.start,
					RawEnd:   source.end,
				})
			}
			return
		}

		diagnostics := make([]logmodel.Diagnostic, 0, 3)
		if cleaned.hadTerminal {
			addDiagnostic(&diagnostics, "terminal_controls_removed", "terminal control sequences or unsafe control characters were removed")
		}
		if cleaned.invalidUTF8 {
			addDiagnostic(&diagnostics, "invalid_utf8_replaced", "invalid UTF-8 bytes were replaced")
		}
		if hasLessTruncation(result.Source[source.start:source.contentEnd], trimmed) {
			addDiagnostic(&diagnostics, "terminal_truncated", "a terminal pager truncation marker indicates missing source text")
		}

		entry, identifiesLogs := parseRecord(cleaned.text, context, diagnostics)
		records = append(records, CapturedRecord{
			Entry:    entry,
			RawStart: source.start,
			RawEnd:   source.end,
		})
		if identifiesLogs {
			recognized = true
		}
	}

	processAvailable := func(final bool) {
		for index := scanPosition; index < len(result.Source); index++ {
			if result.Source[index] != '\n' && result.Source[index] != '\r' {
				continue
			}
			contentEnd := index
			end := index + 1
			if result.Source[index] == '\r' {
				if end == len(result.Source) && !final {
					scanPosition = index
					return
				}
				if end < len(result.Source) && result.Source[end] == '\n' {
					end++
				}
			}
			processFrame(frame{start: frameStart, contentEnd: contentEnd, end: end})
			frameStart = end
			index = end - 1
		}
		scanPosition = len(result.Source)
		if final && frameStart < len(result.Source) {
			processFrame(frame{start: frameStart, contentEnd: len(result.Source), end: len(result.Source)})
			frameStart = len(result.Source)
		}
	}

	emitReady := func() {
		if !recognized || emitted == len(records) || emit == nil {
			return
		}
		batch := append([]CapturedRecord(nil), records[emitted:]...)
		emitted = len(records)
		emit(batch)
	}

	var readErr error
	for {
		readCount, err := reader.Read(buffer)
		if readCount > 0 {
			result.Source = append(result.Source, buffer[:readCount]...)
			processAvailable(false)
			emitReady()
		}
		if err != nil {
			readErr = err
			break
		}
		if readCount == 0 {
			continue
		}
	}

	processAvailable(true)
	emitReady()
	if recognized {
		result.Kind = ResultParsed
		result.Parsed = &ParsedResult{Records: records}
	} else {
		result.Kind = ResultRaw
		result.Raw = &RawResult{Text: sanitizeBytes(result.Source, true).text}
	}

	if readErr != nil && readErr != io.EOF {
		result.Diagnostics = append(result.Diagnostics, LoadDiagnostic{
			Diagnostic: logmodel.Diagnostic{Code: "input_read_error", Message: readErr.Error()},
			RawStart:   len(result.Source),
			RawEnd:     len(result.Source),
		})
		return result, fmt.Errorf("read log input: %w", readErr)
	}
	return result, nil
}

func (e *Engine) context() parseContext {
	location := e.options.DefaultLocation
	if location == nil {
		location = time.UTC
	}
	reference := e.options.ReferenceTime
	if reference.IsZero() {
		reference = time.Now()
	}
	return parseContext{location: location, reference: reference.In(location)}
}
