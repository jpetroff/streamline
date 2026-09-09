//go:build dev

package configuration

import "path/filepath"

func defaultDirectory() (string, error) { return filepath.Abs(".local/streamline") }
