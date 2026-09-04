package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"streamline/internal/query"
)

func TestRawInputHTTPContractAndValidation(t *testing.T) {
	service := query.NewMemoryService(nil)
	service.SetRawOutput("report\n", query.InputEOF, nil)
	server := httptest.NewServer(NewHandler(http.NotFoundHandler(), service))
	t.Cleanup(server.Close)

	response, err := server.Client().Get(server.URL + "/api/v1/input/raw?generation=1&offset=0&limit=1")
	if err != nil {
		t.Fatal(err)
	}
	var page query.RawChunkPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(page.Chunks) != 1 || page.Chunks[0] != "report\n" {
		t.Fatalf("raw response = status %d page %#v", response.StatusCode, page)
	}

	for _, test := range []struct {
		query  string
		status int
		code   string
	}{
		{"", http.StatusBadRequest, "generation_required"},
		{"?generation=stale", http.StatusConflict, "generation_changed"},
		{"?generation=1&limit=17", http.StatusBadRequest, "invalid_limit"},
		{"?generation=1&offset=-1", http.StatusBadRequest, "invalid_offset"},
	} {
		response, err := server.Client().Get(server.URL + "/api/v1/input/raw" + test.query)
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Error query.APIError `json:"error"`
		}
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != test.status || envelope.Error.Code != test.code {
			t.Fatalf("%s = status %d code %q", test.query, response.StatusCode, envelope.Error.Code)
		}
	}
}
