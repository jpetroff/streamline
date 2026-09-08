//go:build linux || darwin

package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"streamline/internal/query"
	"streamline/internal/source"
)

func sourceRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Streamline-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestSourceHTTPIsolationAndLegacyAliases(t *testing.T) {
	m := source.New()
	defer m.Close()
	h := NewSourceHandler(http.NotFoundHandler(), m)
	w := sourceRequest(h, "POST", "/api/v1/sources", `{"command":"printf 'hello\\n'","mode":"text"}`)
	if w.Code != 202 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var info source.Info
	json.Unmarshal(w.Body.Bytes(), &info)
	path := "/api/v1/sources/" + info.ID
	w = sourceRequest(h, "POST", path+"/queries", `{}`)
	if w.Code != 202 {
		t.Fatalf("scoped query: %d %s", w.Code, w.Body)
	}
	var state query.State
	json.Unmarshal(w.Body.Bytes(), &state)
	for _, test := range []struct {
		path string
		want int
	}{
		{path + "/session", 200}, {"/api/v1/session", 200}, {"/api/v1/sources/stdin/session", 200},
		{path + "/queries/" + state.QueryID, 200}, {"/api/v1/queries/" + state.QueryID, 404},
		{"/api/v1/sources/stdin/queries/" + state.QueryID, 404}, {"/api/v1/sources/missing/session", 404},
	} {
		if got := sourceRequest(h, "GET", test.path, ""); got.Code != test.want {
			t.Fatalf("%s = %d %s", test.path, got.Code, got.Body)
		}
	}
	for _, method := range []string{"POST", "DELETE"} {
		route := "/api/v1/sources/stdin"
		if method == "POST" {
			route += "/stop"
		}
		if got := sourceRequest(h, method, route, `{}`); got.Code != 400 {
			t.Fatalf("stdin %s = %d", method, got.Code)
		}
	}
	if got := sourceRequest(h, "POST", path+"/stop", `{}`); got.Code != 204 {
		t.Fatalf("stop = %d", got.Code)
	}
	if got := sourceRequest(h, "DELETE", path, `{}`); got.Code != 204 {
		t.Fatalf("delete = %d %s", got.Code, got.Body)
	}
	if got := sourceRequest(h, "GET", path+"/session", ""); got.Code != 404 {
		t.Fatal("removed source accessible")
	}
}

func TestSourceRequestBoundary(t *testing.T) {
	m := source.New()
	defer m.Close()
	h := NewSourceHandler(http.NotFoundHandler(), m)
	for _, test := range []struct {
		name, host, origin, fetchSite, contentType, header, body string
		want                                                     int
	}{
		{name: "mismatched host", host: "shared.example", origin: "https://evil.example", contentType: "application/json", header: "1", body: `{"command":"true"}`, want: 403},
		{name: "external origin", origin: "https://evil.example", contentType: "application/json", header: "1", body: `{"command":"true"}`, want: 403},
		{name: "null origin", origin: "null", contentType: "application/json", header: "1", body: `{"command":"true"}`, want: 403},
		{name: "other port", origin: "http://localhost:9999", contentType: "application/json", header: "1", body: `{"command":"true"}`, want: 403},
		{name: "cross site", fetchSite: "cross-site", contentType: "application/json", header: "1", body: `{"command":"true"}`, want: 403},
		{name: "form", contentType: "application/x-www-form-urlencoded", body: `command=true`, want: 415},
		{name: "no header", contentType: "application/json", body: `{"command":"true"}`, want: 415},
		{name: "blank", contentType: "application/json", header: "1", body: `{"command":" "}`, want: 400},
		{name: "bad mode", contentType: "application/json", header: "1", body: `{"command":"true","mode":"bad"}`, want: 400},
		{name: "unknown field", contentType: "application/json", header: "1", body: `{"command":"true","extra":1}`, want: 400},
		{name: "trailing json", contentType: "application/json", header: "1", body: `{"command":"true"}{}`, want: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://localhost/api/v1/sources", strings.NewReader(test.body))
			if test.host != "" {
				r.Host = test.host
			}
			r.Header.Set("Origin", test.origin)
			r.Header.Set("Sec-Fetch-Site", test.fetchSite)
			r.Header.Set("Content-Type", test.contentType)
			r.Header.Set("X-Streamline-Request", test.header)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status = %d %s", w.Code, w.Body)
			}
		})
	}
	if len(m.List()) != 1 {
		t.Fatal("rejected requests spawned commands")
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/v1/sources", strings.NewReader(`{"command":"true"}`))
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Streamline-Request", "1")
	w := httptest.NewRecorder()
	NewSourceHandler(http.NotFoundHandler(), m, "http://localhost:5173").ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("trusted dev proxy = %d %s", w.Code, w.Body)
	}
}

func TestSourceEventsInitialLifecycleAndReconnect(t *testing.T) {
	m := source.New()
	defer m.Close()
	server := httptest.NewServer(NewSourceHandler(http.NotFoundHandler(), m))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connect := func() (io.ReadCloser, *bufio.Reader) {
		request, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/sources/events", nil)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			t.Fatal(response.Status)
		}
		return response.Body, bufio.NewReader(response.Body)
	}
	read := func(reader *bufio.Reader) []source.Info {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data: ") {
				var sources []source.Info
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &sources); err != nil {
					t.Fatal(err)
				}
				return sources
			}
		}
	}
	body, reader := connect()
	if sources := read(reader); len(sources) != 1 || sources[0].ID != "stdin" {
		t.Fatalf("initial = %+v", sources)
	}
	info, err := m.Create(source.CreateRequest{Command: `printf 'done\n'`, Mode: "text"})
	if err != nil {
		t.Fatal(err)
	}
	for {
		sources := read(reader)
		if len(sources) == 2 && sources[1].State == "exited" {
			break
		}
	}
	body.Close()
	body, reader = connect()
	defer body.Close()
	if sources := read(reader); len(sources) != 2 || sources[1].ID != info.ID || sources[1].State != "exited" {
		t.Fatalf("reconnect = %+v", sources)
	}
}

func TestSourceRequestOrigins(t *testing.T) {
	const coderHost = "5173--main--stream-logger--eugene.coder.intranet"
	for _, test := range []struct {
		name, host, origin string
		trusted            []string
		want               int
	}{
		{name: "localhost", host: "localhost:8080", origin: "http://localhost:8080", want: 204},
		{name: "IPv6 loopback", host: "[::1]:8080", origin: "http://[::1]:8080", want: 204},
		{name: "shared host read", host: coderHost, want: 204},
		{name: "Coder same origin", host: coderHost, origin: "https://" + coderHost, want: 204},
		{name: "custom same origin", host: "streamline.example", origin: "https://streamline.example", want: 204},
		{name: "local dev proxy", host: "127.0.0.1:8080", origin: "http://localhost:5173", trusted: []string{"http://localhost:5173"}, want: 204},
		{name: "Coder dev proxy", host: "127.0.0.1:8080", origin: "https://" + coderHost, trusted: []string{"https://*.coder.intranet"}, want: 204},
		{name: "another shared port", host: "127.0.0.1:8080", origin: "https://5174--other--workspace--user.coder.intranet", trusted: []string{"https://*.coder.intranet"}, want: 204},
		{name: "release proxy untrusted", host: "127.0.0.1:8080", origin: "https://" + coderHost, want: 403},
		{name: "lookalike prefix", origin: "https://evilcoder.intranet", trusted: []string{"https://*.coder.intranet"}, want: 403},
		{name: "lookalike suffix", origin: "https://port.coder.intranet.evil.example", trusted: []string{"https://*.coder.intranet"}, want: 403},
		{name: "bare domain", origin: "https://coder.intranet", trusted: []string{"https://*.coder.intranet"}, want: 403},
		{name: "wrong scheme", origin: "http://" + coderHost, trusted: []string{"https://*.coder.intranet"}, want: 403},
		{name: "wrong port", origin: "https://" + coderHost + ":8443", trusted: []string{"https://*.coder.intranet"}, want: 403},
		{name: "credentials", origin: "https://user@" + coderHost, trusted: []string{"https://*.coder.intranet"}, want: 403},
		{name: "path", origin: "https://" + coderHost + "/path", trusted: []string{"https://*.coder.intranet"}, want: 403},
		{name: "query", origin: "https://" + coderHost + "?query", trusted: []string{"https://*.coder.intranet"}, want: 403},
		{name: "fragment", origin: "https://" + coderHost + "#", trusted: []string{"https://*.coder.intranet"}, want: 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := sourceRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}), test.trusted)
			method := http.MethodPost
			if test.name == "shared host read" {
				method = http.MethodGet
			}
			r := httptest.NewRequest(method, "http://localhost/api/v1/sources", nil)
			if test.host != "" {
				r.Host = test.host
			}
			r.Header.Set("Origin", test.origin)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Streamline-Request", "1")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, test.want, w.Body)
			}
		})
	}
}
