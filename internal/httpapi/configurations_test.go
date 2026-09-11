package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"streamline/internal/configuration"
	"streamline/internal/source"
)

const savedDocument = `{"version":1,"name":"Example","command":"echo hello","mode":"text","columns":[{"path":"message","dateFormat":"original","width":280.5}],"filters":{"filter":[],"search":{"text":"","mode":"plain","operator":"or"}}}`

func TestConfigurationAPI(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "settings")
	store := configuration.New(directory)
	manager := source.New()
	defer manager.Close()
	handler := NewConfiguredSourceHandler(http.NotFoundHandler(), manager, store)
	request := func(method, path, body string, headers bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		if headers {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Streamline-Request", "1")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	validated := request("POST", "/api/v1/configurations/validate", savedDocument, true)
	if validated.Code != 200 {
		t.Fatalf("validate: %d %s", validated.Code, validated.Body)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("validation wrote files")
	}
	if len(manager.List()) != 1 {
		t.Fatal("validation ran a command")
	}
	if got := request("POST", "/api/v1/configurations", savedDocument, false); got.Code != 415 {
		t.Fatalf("unprotected: %d", got.Code)
	}
	r := httptest.NewRequest("POST", "http://localhost/api/v1/configurations", strings.NewReader(savedDocument))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("origin: %d", w.Code)
	}
	created := request("POST", "/api/v1/configurations", savedDocument, true)
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	var entry configuration.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/configurations/" + entry.ID
	if got := request("GET", path, "", false); got.Code != 200 {
		t.Fatalf("get: %d %s", got.Code, got.Body)
	}
	if got := request("PUT", path, strings.Replace(savedDocument, "Example", "Changed", 1), true); got.Code != 200 {
		t.Fatalf("update: %d %s", got.Code, got.Body)
	}
	invalid := strings.Replace(savedDocument, `"text":""`, `"text":"["`, 1)
	invalid = strings.Replace(invalid, `"mode":"plain"`, `"mode":"regexp"`, 1)
	got := request("PUT", path, invalid, true)
	if got.Code != 400 || !strings.Contains(got.Body.String(), `"line":1`) {
		t.Fatalf("regex error: %d %s", got.Code, got.Body)
	}
	if got := request("GET", path, "", false); !strings.Contains(got.Body.String(), "Changed") {
		t.Fatal("invalid update changed entry")
	}
	if got := request("PUT", "/api/v1/configurations/missing", savedDocument, true); got.Code != 404 {
		t.Fatalf("missing: %d", got.Code)
	}
	if err := os.WriteFile(filepath.Join(directory, "configs", "broken.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := request("GET", "/api/v1/configurations", "", false); got.Code != 200 || !strings.Contains(got.Body.String(), "broken.json") {
		t.Fatalf("list: %d %s", got.Code, got.Body)
	}
	if len(manager.List()) != 1 {
		t.Fatal("saving ran a command")
	}
	if got := request("DELETE", path, "", false); got.Code != 415 {
		t.Fatalf("unprotected delete: %d", got.Code)
	}
	if got := request("GET", path, "", false); got.Code != 200 {
		t.Fatal("unprotected delete removed entry")
	}
	if got := request("DELETE", path, "", true); got.Code != 204 || got.Body.Len() != 0 {
		t.Fatalf("delete: %d %s", got.Code, got.Body)
	}
	if got := request("GET", path, "", false); got.Code != 404 {
		t.Fatalf("get deleted: %d", got.Code)
	}
	if got := request("DELETE", path, "", true); got.Code != 404 {
		t.Fatalf("delete missing: %d", got.Code)
	}

}
func TestConfigurationStorageFailureDoesNotBreakViewer(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := source.New()
	defer manager.Close()
	handler := NewConfiguredSourceHandler(http.NotFoundHandler(), manager, configuration.New(file))
	for path, status := range map[string]int{"/api/v1/configurations": 500, "/api/v1/health": 200, "/api/v1/session": 200} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
}
