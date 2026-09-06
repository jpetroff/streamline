package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"streamline/internal/logmodel"
	"streamline/internal/query"
)

func waitReady(t *testing.T, client *http.Client, url string) query.State {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		var state query.State
		err = json.NewDecoder(response.Body).Decode(&state)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if state.Status == query.StatusReady {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("query did not become ready")
	return query.State{}
}

func TestQueryHTTPContractAndStableSnapshotPage(t *testing.T) {
	service := query.NewMemoryService(nil)
	service.Append([]query.Record{{
		Message: "first", SourceFormat: logmodel.FormatJSON,
		Fields:      map[string]any{"nested": map[string]any{"ok": true}, "values": []any{"one", json.Number("2")}},
		Diagnostics: []logmodel.Diagnostic{{Code: "normalized", Message: "normalized"}},
	}, {Message: "second"}})
	server := httptest.NewServer(NewHandler(http.NotFoundHandler(), service))
	t.Cleanup(server.Close)

	body := bytes.NewBufferString(`{"filter":[],"sort":"input"}`)
	response, err := http.Post(server.URL+"/api/v1/queries", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var created query.State
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	ready := waitReady(t, server.Client(), server.URL+"/api/v1/queries/"+created.QueryID)

	pageURL := server.URL + "/api/v1/queries/" + created.QueryID + "/rows?snapshot=" + ready.Snapshot.SnapshotToken + "&offset=0&limit=1"
	pageResponse, err := server.Client().Get(pageURL)
	if err != nil {
		t.Fatal(err)
	}
	var page query.Page
	if err := json.NewDecoder(pageResponse.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	pageResponse.Body.Close()
	if len(page.Rows) != 1 || page.Rows[0].ID != "1" || page.Snapshot.Revision != ready.Snapshot.Revision {
		t.Fatalf("unexpected page: %#v", page)
	}

	row := page.Rows[0]
	if row.SourceFormat != logmodel.FormatJSON ||
		row.Fields["nested"].(map[string]any)["ok"] != true ||
		row.Diagnostics[0].Code != "normalized" {
		t.Fatalf("structured row contract = %#v", row)
	}

	invalid, err := server.Client().Get(server.URL + "/api/v1/queries/" + created.QueryID + "/rows?snapshot=missing")
	if err != nil {
		t.Fatal(err)
	}
	defer invalid.Body.Close()
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid snapshot status = %d", invalid.StatusCode)
	}
	var errorEnvelope struct {
		Error query.APIError `json:"error"`
	}
	if err := json.NewDecoder(invalid.Body).Decode(&errorEnvelope); err != nil {
		t.Fatal(err)
	}
	if errorEnvelope.Error.Code != "snapshot_invalid" {
		t.Fatalf("code = %q", errorEnvelope.Error.Code)
	}
}

func TestEventStreamStartsWithAuthoritativeState(t *testing.T) {
	service := query.NewMemoryService(nil)
	created, err := service.Create(context.Background(), query.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(http.NotFoundHandler(), service))
	t.Cleanup(server.Close)

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/queries/"+created.QueryID+"/events", nil)
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	response, err := server.Client().Do(request.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type = %q", response.Header.Get("Content-Type"))
	}

	reader := bufio.NewReader(response.Body)
	var data string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		}
		if line == "\n" {
			break
		}
	}
	var event query.Event
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "state" || event.State.QueryID != created.QueryID || event.Session.SessionID == "" {
		t.Fatalf("unexpected initial event: %#v", event)
	}
}

func TestRequestValidationUsesStructuredErrors(t *testing.T) {
	server := httptest.NewServer(NewHandler(http.NotFoundHandler(), query.NewMemoryService(nil)))
	t.Cleanup(server.Close)
	response, err := http.Post(server.URL+"/api/v1/queries", "application/json", strings.NewReader(`{"sort":"other"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusBadRequest || !bytes.Contains(payload, []byte(`"code":"invalid_sort"`)) {
		t.Fatalf("status %d body %s", response.StatusCode, payload)
	}
}

func TestSharedQueryEventFixtureMatchesGoContract(t *testing.T) {
	data, err := os.ReadFile("../../testdata/transport/query-event.json")
	if err != nil {
		t.Fatal(err)
	}
	var event query.Event
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	if event.State.Snapshot.ProcessedThrough != "9007199254740993" || event.State.Snapshot.QueryID != event.State.QueryID {
		t.Fatalf("unexpected fixture: %#v", event)
	}
}

func TestSearchHTTPValidationAndMatching(t *testing.T) {
	service := query.NewMemoryService(nil)
	service.Append([]query.Record{{Message: "timeout", Fields: map[string]any{"nested": map[string]any{"host": "api"}}}, {Message: "healthy"}})
	handler := NewHandler(http.NotFoundHandler(), service)
	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/api/v1/queries", strings.NewReader(`{"search":{"text":"ok\n\n[\n(?=x)","mode":"regexp"}}`)))
	var envelope struct {
		Error query.APIError `json:"error"`
	}
	if err := json.Unmarshal(invalid.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if invalid.Code != 400 || envelope.Error.Code != "invalid_search" || len(envelope.Error.LineErrors) != 2 || envelope.Error.LineErrors[0].Line != 3 || envelope.Error.LineErrors[1].Line != 4 {
		t.Fatalf("response %d %s", invalid.Code, invalid.Body)
	}
	valid := httptest.NewRecorder()
	handler.ServeHTTP(valid, httptest.NewRequest(http.MethodPost, "/api/v1/queries", strings.NewReader(`{"filter":[],"sort":"input","search":{"text":"TIMEOUT\napi","operator":"and"}}`)))
	if valid.Code != 202 {
		t.Fatalf("response %d %s", valid.Code, valid.Body)
	}
	var state query.State
	if err := json.Unmarshal(valid.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	ready := waitReady(t, server.Client(), server.URL+"/api/v1/queries/"+state.QueryID)
	if ready.Snapshot.MatchedCount != "1" {
		t.Fatalf("snapshot %#v", ready.Snapshot)
	}
}

func TestFilterHTTPValidationAndNumericDecoding(t *testing.T) {
	service := query.NewMemoryService(nil)
	handler := NewHandler(http.NotFoundHandler(), service)
	for _, body := range []string{
		`{"filter":null}`, `{"filter":"old expression"}`, `{"filter":{}}`,
		`{"filter":[{"field":"n","op":"gt","value":"10"}]}`,
		`{"filter":[{"field":"x","op":"eq","value":"ok"},{"field":"x","op":"regex","value":"(?=x)"}]}`,
		`{"filter":[{"field":"x","op":"eq","value":"ok","extra":true}]}`,
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/queries", strings.NewReader(body)))
		var envelope struct {
			Error query.APIError `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if response.Code != 400 || envelope.Error.Code != "invalid_filter" || len(envelope.Error.FilterErrors) == 0 {
			t.Fatalf("response = %d %s", response.Code, response.Body.String())
		}
		if strings.Contains(body, "(?=x)") && (envelope.Error.FilterErrors[0].Index != 2 || envelope.Error.FilterErrors[0].Property != "value") {
			t.Fatalf("wrong error position: %#v", envelope.Error.FilterErrors)
		}
	}
	service.Append([]query.Record{{Fields: map[string]any{"n": json.Number("12")}}, {Fields: map[string]any{"n": "12"}}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/queries", strings.NewReader(`{"filter":[{"field":"n","op":"gt","value":10}],"sort":"input"}`)))
	if response.Code != http.StatusAccepted {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	var created query.State
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, err := service.Get(context.Background(), created.QueryID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status == query.StatusReady {
			if state.Snapshot.MatchedCount != "1" {
				t.Fatalf("numeric matches = %s", state.Snapshot.MatchedCount)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("query did not become ready")
}
