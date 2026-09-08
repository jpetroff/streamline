//go:build linux || darwin

package source

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	"streamline/internal/ingest"
	"streamline/internal/parse"
	"streamline/internal/query"
)

const supported = true
const stopGrace = 2 * time.Second
const drainGrace = time.Second

func (m *Manager) run(s *input) {
	defer close(s.done)
	reader, writer, err := os.Pipe()
	if err != nil {
		m.finish(s, ingest.Completion{}, nil, sourceError("command_start_failed", err.Error()))
		return
	}
	defer reader.Close()
	cmd := exec.Command(m.shellPath, "-c", s.info.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = writer, writer
	// A nil Stdin is connected to the null device by os/exec.
	if err := cmd.Start(); err != nil {
		_ = writer.Close()
		m.finish(s, ingest.Completion{}, nil, sourceError("command_start_failed", err.Error()))
		return
	}
	_ = writer.Close()
	m.mu.Lock()
	if !s.stopRequested {
		s.info.State = "running"
		m.changedLocked()
	}
	m.mu.Unlock()
	captured := make(chan ingest.Completion, 1)
	go func() {
		captured <- ingest.Capture(context.Background(), reader, parse.NewEngine(parse.Options{Text: s.info.Mode == "text"}), &sourceSink{manager: m, input: s})
	}()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	signalGroup := func(signal syscall.Signal) { _ = syscall.Kill(-cmd.Process.Pid, signal) }
	var completion ingest.Completion
	var waitErr error
	var processDone, captureDone, stopping bool
	var deadline <-chan time.Time
	var timer *time.Timer
	setDeadline := func(delay time.Duration) {
		if timer != nil {
			timer.Stop()
		}
		timer = time.NewTimer(delay)
		deadline = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	stop := s.stop
	var terminalErr *query.APIError
	for !processDone || !captureDone || stopping {
		select {
		case <-stop:
			stop = nil
			stopping = true
			signalGroup(syscall.SIGTERM)
			setDeadline(stopGrace)
		case waitErr = <-waited:
			waited = nil
			processDone = true
			if !stopping && !captureDone {
				setDeadline(stopGrace)
			}
		case completion = <-captured:
			captured = nil
			captureDone = true
		case <-deadline:
			deadline = nil
			signalGroup(syscall.SIGKILL)
			if stopping {
				stopping = false
			}
			if !captureDone {
				// Give killed writers time to close normally before forcing reader closure.
				select {
				case completion = <-captured:
					captured = nil
					captureDone = true
				case <-time.After(drainGrace):
					_ = reader.Close()
					completion = <-captured
					captured = nil
					captureDone = true
					terminalErr = sourceError("command_output_timeout", "command output did not close after the process exited")
				}
			}
		}
	}
	// The process may exit and drain at the same instant Stop is requested.
	// Finish takes the manager lock to resolve that race consistently.
	code := cmd.ProcessState.ExitCode()
	if waitErr != nil && terminalErr == nil {
		terminalErr = sourceError("command_exit_failed", waitErr.Error())
	}
	m.finish(s, completion, &code, terminalErr)
}
