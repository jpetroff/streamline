package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"streamline/internal/logmodel"
	"streamline/internal/parse"
	"streamline/internal/query"
)

func TestParserExampleConfigurationsMatchSampleLogs(t *testing.T) {
	for _, format := range []logmodel.SourceFormat{
		logmodel.FormatSyslogRFC5424, logmodel.FormatSyslogRFC3164, logmodel.FormatSyslogText, logmodel.FormatHTTPAccess,
	} {
		t.Run(string(format), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../../examples/configurations", string(format)+".json"))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			predicate, err := (query.TupleCompiler{}).Compile(doc.Filters.Filter)
			if err != nil {
				t.Fatal(err)
			}
			log, err := os.ReadFile(filepath.Join("../../examples/logs", string(format)+".log"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := parse.NewEngine(parse.Options{}).Load(strings.NewReader(string(log)))
			if err != nil || result.Kind != parse.ResultParsed {
				t.Fatalf("sample logs: %v", err)
			}
			matches := 0
			for _, captured := range result.Parsed.Records {
				if captured.Entry.SourceFormat != format {
					t.Fatalf("format = %s", captured.Entry.SourceFormat)
				}
				if predicate(captured.Entry) {
					matches++
				}
			}
			if matches != 1 {
				t.Fatalf("example matched %d records, want 1", matches)
			}
		})
	}
}
