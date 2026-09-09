//go:build !dev

package configuration

import (
	"os"
	"path/filepath"
)

func defaultDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "streamline"), nil
}
