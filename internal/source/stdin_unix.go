//go:build linux || darwin

package source

import (
	"io"
	"os"
	"syscall"
)

// OpenStdin makes a pollable duplicate so shutdown can interrupt a pending read.
// Standard os.Stdin may have been initialized as a blocking, non-pollable file.
func OpenStdin() (io.ReadCloser, error) { return interruptibleFile(os.Stdin) }

func interruptibleFile(original *os.File) (io.ReadCloser, error) {
	originalFD := int(original.Fd())
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(originalFD), syscall.F_GETFL, 0)
	if errno != 0 {
		return nil, errno
	}
	fd, err := syscall.Dup(originalFD)
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(fd)
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return &stdinReader{File: os.NewFile(uintptr(fd), "stdin"), originalFD: originalFD, nonblock: flags&syscall.O_NONBLOCK != 0}, nil
}

type stdinReader struct {
	*os.File
	originalFD int
	nonblock   bool
}

func (r *stdinReader) Close() error {
	err := r.File.Close()
	// Dup shares status flags with the original descriptor (and a parent tty).
	// Restore its original mode only after the pollable read has been interrupted.
	_ = syscall.SetNonblock(r.originalFD, r.nonblock)
	return err
}
