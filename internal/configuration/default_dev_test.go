//go:build dev

package configuration

import (
	"path/filepath"
	"testing"
)

func TestDevelopmentDefault(t *testing.T) {
	got, err := ResolveDirectory("")
	want, _ := filepath.Abs(".local/streamline")
	if err != nil || got != want {
		t.Fatalf("default %q %v", got, err)
	}
}
