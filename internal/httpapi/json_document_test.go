//go:build linux || darwin

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"streamline/internal/logmodel"
	"streamline/internal/query"
	"streamline/internal/source"
)

func TestJSONDocumentSourceAndQueriesThroughHTTP(t *testing.T) {
	m := source.New()
	t.Cleanup(m.Close)
	handler := NewSourceHandler(http.NotFoundHandler(), m)
	command := "printf '%s' '[\n" +
		`{"message":"blocked","events_count":2,"source":{"ip":"192.0.2.1"},"events":[{"kind":"waf"}],"id":9007199254740993},` + "\n" +
		`{"message":"allowed","events_count":1,"source":{"ip":"192.0.2.2"},"events":[],"id":2}` + "\n]'"
	body, err := json.Marshal(source.CreateRequest{Command: command, Mode: "json-document"})
	if err != nil {
		t.Fatal(err)
	}
	response := sourceRequest(handler, "POST", "/api/v1/sources", string(body))
	if response.Code != http.StatusAccepted {
		t.Fatalf("create source: %d %s", response.Code, response.Body)
	}
	var info source.Info
	if err := json.Unmarshal(response.Body.Bytes(), &info); err != nil || info.Mode != "json-document" {
		t.Fatalf("source = %#v, %v", info, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response = sourceRequest(handler, "GET", "/api/v1/sources", "")
		var sources []source.Info
		if err := json.Unmarshal(response.Body.Bytes(), &sources); err != nil {
			t.Fatal(err)
		}
		for _, current := range sources {
			if current.ID == info.ID {
				info = current
			}
		}
		if info.State == "exited" {
			break
		}
		if info.State == "failed" || time.Now().After(deadline) {
			t.Fatalf("source did not complete: %#v", info)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if info.Mode != "json-document" || info.Session.InputKind != query.InputRecords || info.Session.InputStatus != query.InputEOF {
		t.Fatalf("source descriptor = %#v", info)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base := server.URL + "/api/v1/sources/" + info.ID
	for _, tc := range []struct {
		name, request string
		ids           []string
	}{
		{"all", `{}`, []string{"1", "2"}},
		{"nested scalar", `{"filter":[{"field":"source.ip","op":"eq","value":"192.0.2.1"}]}`, []string{"1"}},
		{"numeric", `{"filter":[{"field":"events_count","op":"gt","value":1}]}`, []string{"1"}},
		{"message search", `{"search":{"text":"allowed","mode":"plain"}}`, []string{"2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := server.Client().Post(base+"/queries", "application/json", strings.NewReader(tc.request))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusAccepted {
				t.Fatalf("query status = %d", response.StatusCode)
			}
			var created query.State
			if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			ready := waitReady(t, server.Client(), base+"/queries/"+created.QueryID)
			pageResponse, err := server.Client().Get(base + "/queries/" + created.QueryID + "/rows?snapshot=" + ready.Snapshot.SnapshotToken)
			if err != nil {
				t.Fatal(err)
			}
			defer pageResponse.Body.Close()
			var page query.Page
			decoder := json.NewDecoder(pageResponse.Body)
			decoder.UseNumber()
			if err := decoder.Decode(&page); err != nil || len(page.Rows) != len(tc.ids) {
				t.Fatalf("page = %#v, %v", page, err)
			}
			for i, row := range page.Rows {
				if row.ID != tc.ids[i] || row.SourceFormat != logmodel.FormatJSON || row.Timestamp != "" || row.Severity != "" {
					t.Fatalf("row = %#v", row)
				}
				if row.ID == "1" && (row.Fields["id"] != json.Number("9007199254740993") || len(row.Fields["events"].([]any)) != 1) {
					t.Fatalf("source types changed: %#v", row.Fields)
				}
			}
		})
	}
}
