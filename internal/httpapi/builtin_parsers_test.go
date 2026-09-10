package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"streamline/internal/ingest"
	"streamline/internal/logmodel"
	"streamline/internal/parse"
	"streamline/internal/query"
)

func TestBuiltInTextParserFieldsThroughHTTP(t *testing.T) {
	input := strings.Join([]string{
		`<35>1 2026-09-10T12:00:00Z home sshd 42 AUTH [origin ip="192.0.2.10"] denied`,
		"<30>Sep 10 12:00:01 home cron[43]: done",
		"2026-09-10T12:00:02Z nas sshd[44]: connected",
		`192.0.2.10 - - [10/Sep/2026:12:00:03 +0000] "GET / HTTP/1.1" 200 2`,
		`2001:db8::10 - alice [10/Sep/2026:12:00:04 +0000] "GET /cloud HTTP/2.0" 503 9007199254740993 "-" "ExampleClient/1.0"`,
		"<999>bad",
		`192.0.2.10 - - [10/Sep/2026:12:00:03 +0000] "bad suffix" 200 2 extra`,
	}, "\n")
	service := query.NewMemoryService(nil)
	ingest.Run(context.Background(), strings.NewReader(input), parse.NewEngine(parse.Options{}), service)
	server := httptest.NewServer(NewHandler(http.NotFoundHandler(), service))
	t.Cleanup(server.Close)
	cases := []struct {
		name, request string
		ids           []string
	}{
		{"all formats", "{}", []string{"1", "2", "3", "4", "5", "6", "7"}},
		{"numeric 5xx", `{"filter":[{"field":"status","op":"gte","value":500},{"field":"status","op":"lt","value":600}]}`, []string{"5"}},
		{"numeric byte filter", `{"filter":[{"field":"bytes","op":"gt","value":1024}]}`, []string{"5"}},
		{"host and app", `{"filter":[{"field":"hostname","op":"eq","value":"HOME"},{"field":"app","op":"eq","value":"sshd"}]}`, []string{"1"}},
		{"local application", `{"filter":[{"field":"app","op":"eq","value":"sshd"}]}`, []string{"1", "3"}},
		{"structured data", `{"filter":[{"field":"structured_data.origin.ip","op":"eq","value":"192.0.2.10"}]}`, []string{"1"}},
		{"search user agent", `{"search":{"text":"exampleclient","mode":"plain"}}`, []string{"5"}},
	}
	formats := []logmodel.SourceFormat{logmodel.FormatSyslogRFC5424, logmodel.FormatSyslogRFC3164, logmodel.FormatSyslogText, logmodel.FormatHTTPAccess, logmodel.FormatHTTPAccess, logmodel.FormatText, logmodel.FormatText}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response, err := server.Client().Post(server.URL+"/api/v1/queries", "application/json", strings.NewReader(tc.request))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusAccepted {
				t.Fatalf("create status = %d", response.StatusCode)
			}
			var created query.State
			if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			ready := waitReady(t, server.Client(), server.URL+"/api/v1/queries/"+created.QueryID)
			response, err = server.Client().Get(server.URL + "/api/v1/queries/" + created.QueryID + "/rows?snapshot=" + ready.Snapshot.SnapshotToken)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("rows status = %d", response.StatusCode)
			}
			var page query.Page
			decoder := json.NewDecoder(response.Body)
			decoder.UseNumber()
			if err := decoder.Decode(&page); err != nil {
				t.Fatal(err)
			}
			if len(page.Rows) != len(tc.ids) {
				t.Fatalf("rows = %#v", page.Rows)
			}
			for i, r := range page.Rows {
				if r.ID != tc.ids[i] || r.SourceFormat != formats[int(r.ID[0]-'1')] {
					t.Fatalf("row = %#v", r)
				}
				switch r.ID {
				case "1":
					if r.Timestamp != "2026-09-10T12:00:00Z" || r.Severity != "error" || r.Message != "denied" || r.Fields["priority"] != json.Number("35") || r.Fields["facility"] != json.Number("4") {
						t.Fatalf("syslog fields = %#v", r)
					}
				case "3":
					if r.Severity != "" || r.Fields["procid"] != "44" {
						t.Fatalf("local fields = %#v", r)
					}
				case "5":
					if r.Severity != "" || r.Fields["status"] != json.Number("503") || r.Fields["bytes"] != json.Number("9007199254740993") || r.Fields["client"] != "2001:db8::10" || r.Fields["target"] != "/cloud" {
						t.Fatalf("access fields = %#v", r)
					}
				case "6", "7":
					code := "malformed_syslog_fallback"
					if r.ID == "7" {
						code = "malformed_http_access_fallback"
					}
					if r.Fields != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != code {
						t.Fatalf("fallback = %#v", r)
					}
				}
			}
		})
	}
}
