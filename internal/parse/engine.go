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

// LoadResult owns the original input once and references it by byte offsets.
type LoadResult struct {
	Raw         []byte
	Records     []CapturedRecord
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

// Load reads the source into memory and normalizes every physical record.
// If the reader fails, bytes read before the failure are still parsed and
// returned together with the wrapped error.
func (e *Engine) Load(reader io.Reader) (*LoadResult, error) {
	raw, readErr := io.ReadAll(reader)
	result := &LoadResult{Raw: raw}
	context := e.context()

	for _, source := range splitFrames(raw) {
		cleaned := sanitizeBytes(raw[source.start:source.contentEnd], false)
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
			continue
		}

		diagnostics := make([]logmodel.Diagnostic, 0, 3)
		if cleaned.hadTerminal {
			addDiagnostic(&diagnostics, "terminal_controls_removed", "terminal control sequences or unsafe control characters were removed")
		}
		if cleaned.invalidUTF8 {
			addDiagnostic(&diagnostics, "invalid_utf8_replaced", "invalid UTF-8 bytes were replaced")
		}
		if hasLessTruncation(raw[source.start:source.contentEnd], trimmed) {
			addDiagnostic(&diagnostics, "terminal_truncated", "a terminal pager truncation marker indicates missing source text")
		}

		result.Records = append(result.Records, CapturedRecord{
			Entry:    parseRecord(cleaned.text, context, diagnostics),
			RawStart: source.start,
			RawEnd:   source.end,
		})
	}

	if readErr != nil {
		result.Diagnostics = append(result.Diagnostics, LoadDiagnostic{
			Diagnostic: logmodel.Diagnostic{Code: "input_read_error", Message: readErr.Error()},
			RawStart:   len(raw),
			RawEnd:     len(raw),
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

func splitFrames(raw []byte) []frame {
	frames := make([]frame, 0)
	start := 0
	for index := 0; index < len(raw); index++ {
		if raw[index] != '\n' && raw[index] != '\r' {
			continue
		}
		contentEnd := index
		end := index + 1
		if raw[index] == '\r' && end < len(raw) && raw[end] == '\n' {
			end++
			index++
		}
		frames = append(frames, frame{start: start, contentEnd: contentEnd, end: end})
		start = end
	}
	if start < len(raw) {
		frames = append(frames, frame{start: start, contentEnd: len(raw), end: len(raw)})
	}
	return frames
}
