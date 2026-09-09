//go:build !dev

package configuration

import (
	"path/filepath"
	"testing"
)

func TestProductionDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got, err := ResolveDirectory("")
	if err != nil || got != filepath.Join(home, ".config", "streamline") {
		t.Fatalf("default %q %v", got, err)
	}
}
