//go:build linux || darwin

package source

import (
	"path/filepath"
	"strings"
	"testing"

	"streamline/internal/logmodel"
)

func TestCommandSourcesUseBuiltInTextParsers(t *testing.T) {
	for _, format := range []logmodel.SourceFormat{
		logmodel.FormatSyslogRFC5424, logmodel.FormatSyslogRFC3164, logmodel.FormatSyslogText, logmodel.FormatHTTPAccess,
	} {
		t.Run(string(format), func(t *testing.T) {
			path, err := filepath.Abs(filepath.Join("../../examples/logs", string(format)+".log"))
			if err != nil {
				t.Fatal(err)
			}
			m := New()
			defer m.Close()
			command := "cat '" + strings.ReplaceAll(path, "'", "'\\''") + "'"
			auto := create(t, m, command, "auto")
			completed(t, m, auto.ID)
			got := rows(t, m, auto.ID)
			if len(got) != 2 {
				t.Fatalf("rows = %#v", got)
			}
			for _, r := range got {
				if r.SourceFormat != format || r.Fields == nil {
					t.Fatalf("auto row = %#v", r)
				}
			}
			literal := create(t, m, command, "text")
			completed(t, m, literal.ID)
			for _, r := range rows(t, m, literal.ID) {
				if r.SourceFormat != logmodel.FormatText || r.Fields != nil {
					t.Fatalf("text row = %#v", r)
				}
			}
		})
	}
}
