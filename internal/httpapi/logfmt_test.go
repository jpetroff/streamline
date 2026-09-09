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

func TestLogfmtIngestionQueryAndHTTPTransport(t *testing.T) {
	service := query.NewMemoryService(nil)
	input := `time="2026-01-03T18:07:22+02:00" level=warning msg="blocked 世界" module=db status=403` + "\n" +
		`status=200 enabled=true` + "\n" + `msg=accepted module=api status=200`
	ingest.Run(context.Background(), strings.NewReader(input), parse.NewEngine(parse.Options{}), service)
	if session := service.Session(context.Background()); session.InputKind != query.InputRecords || session.InputStatus != query.InputEOF {
		t.Fatalf("session = %#v", session)
	}
	server := httptest.NewServer(NewHandler(http.NotFoundHandler(), service))
	t.Cleanup(server.Close)
	cases := []struct {
		name, request string
		ids           []string
	}{
		{"string filter and field search", `{"filter":[{"field":"status","op":"eq","value":"403"}],"search":{"text":"DB","mode":"plain"}}`, []string{"1"}},
		{"Unicode message search", `{"search":{"text":"世界","mode":"plain"}}`, []string{"1"}},
		{"no numeric coercion", `{"filter":[{"field":"status","op":"gte","value":400}]}`, nil},
		{"field-only fallback searches values", `{"search":{"text":"true","mode":"plain"}}`, []string{"2"}},
		{"field-only fallback excludes keys", `{"search":{"text":"enabled","mode":"plain"}}`, nil},
	}
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
			pageResponse, err := server.Client().Get(server.URL + "/api/v1/queries/" + created.QueryID + "/rows?snapshot=" + ready.Snapshot.SnapshotToken)
			if err != nil {
				t.Fatal(err)
			}
			defer pageResponse.Body.Close()
			if pageResponse.StatusCode != http.StatusOK {
				t.Fatalf("page status = %d", pageResponse.StatusCode)
			}
			var page query.Page
			if err := json.NewDecoder(pageResponse.Body).Decode(&page); err != nil {
				t.Fatal(err)
			}
			if len(page.Rows) != len(tc.ids) {
				t.Fatalf("rows = %#v", page.Rows)
			}
			for i, row := range page.Rows {
				if row.ID != tc.ids[i] || row.SourceFormat != logmodel.FormatLogfmt {
					t.Fatalf("row = %#v", row)
				}
				if row.ID == "1" && (row.Timestamp != "2026-01-03T16:07:22Z" || row.Severity != "warn" || row.Message != "blocked 世界" || row.Fields["status"] != "403" || row.Fields["level"] != "warning" || row.Fields["module"] != "db") {
					t.Fatalf("canonical/source fields = %#v", row)
				}
				if row.ID == "2" && (row.Fields["enabled"] != "true" || row.Message != `{"enabled":"true","status":"200"}` || len(row.Diagnostics) != 1 || row.Diagnostics[0].Code != "missing_message") {
					t.Fatalf("fallback row = %#v", row)
				}
			}
		})
	}
}
