//go:build !linux && !darwin

package source

import (
	"io"
	"os"
)

func OpenStdin() (io.ReadCloser, error) { return os.Stdin, nil }
