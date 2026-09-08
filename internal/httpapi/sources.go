package httpapi

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"streamline/internal/query"
	"streamline/internal/source"
)

// NewSourceHandler adds source management and source-scoped query routes. Legacy
// routes remain aliases for stdin. Additional trusted origins are development-only
// and may use a wildcard subdomain, such as https://*.coder.intranet.
func NewSourceHandler(frontend http.Handler, manager *source.Manager, trustedOrigins ...string) http.Handler {
	stdin, _ := manager.Queries("stdin")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/sources", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, manager.List()) })
	mux.HandleFunc("GET /api/v1/sources/events", func(w http.ResponseWriter, r *http.Request) {
		events, unsubscribe := manager.Subscribe()
		defer unsubscribe()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		controller := http.NewResponseController(w)
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case _, ok := <-events:
				if !ok {
					return
				}
				data, err := json.Marshal(manager.List())
				if err != nil {
					return
				}
				_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, err := w.Write(append(append([]byte("event: sources\ndata: "), data...), '\n', '\n')); err != nil {
					return
				}
				if controller.Flush() != nil {
					return
				}
			case <-heartbeat.C:
				_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
					return
				}
				if controller.Flush() != nil {
					return
				}
			}
		}
	})
	mux.HandleFunc("POST /api/v1/sources", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var request source.CreateRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeError(w, 400, &query.APIError{Code: "invalid_json", Message: "request must contain a command and optional mode"})
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeError(w, 400, &query.APIError{Code: "invalid_json", Message: "request must contain one JSON value"})
			return
		}
		info, err := manager.Create(request)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/sources/"+info.ID+"/session")
		writeJSON(w, http.StatusAccepted, info)
	})
	mux.HandleFunc("POST /api/v1/sources/{source}/stop", func(w http.ResponseWriter, r *http.Request) {
		if !emptySourceBody(w, r) {
			return
		}
		if err := manager.Stop(r.PathValue("source")); err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/sources/{source}", func(w http.ResponseWriter, r *http.Request) {
		if !emptySourceBody(w, r) {
			return
		}
		if err := manager.Remove(r.PathValue("source")); err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/api/v1/sources/{source}/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("source")
		service, err := manager.Queries(id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		forwarded := r.Clone(r.Context())
		forwarded.URL.Path = "/api/v1" + strings.TrimPrefix(r.URL.Path, "/api/v1/sources/"+id)
		forwarded.URL.RawPath = ""
		NewHandler(http.NotFoundHandler(), service).ServeHTTP(w, forwarded)
	}))
	mux.Handle("/", NewHandler(frontend, stdin))
	return sourceRequests(mux, trustedOrigins)
}

func emptySourceBody(w http.ResponseWriter, r *http.Request) bool {
	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil || (len(strings.TrimSpace(string(body))) != 0 && strings.TrimSpace(string(body)) != "{}") {
		writeError(w, 400, &query.APIError{Code: "invalid_json", Message: "request body must be empty or an empty JSON object"})
		return false
	}
	return true
}

func sourceRequests(next http.Handler, trustedOrigins []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		if mutation {
			origin := r.Header.Get("Origin")
			allowed := origin == "" && r.Header.Get("Sec-Fetch-Site") != "cross-site"
			if parsed, err := url.Parse(origin); err == nil && validOrigin(parsed, origin) {
				allowed = parsed.Host == r.Host
				for _, trusted := range trustedOrigins {
					if matchesTrustedOrigin(parsed, trusted) {
						allowed = true
					}
				}
			}
			if !allowed {
				writeError(w, 403, &query.APIError{Code: "invalid_origin", Message: "a same-origin request is required"})
				return
			}
			// A required non-simple header also prevents form posts and unapproved CORS
			// requests from invoking source lifecycle operations without an Origin.
			sourceMutation := r.URL.Path == "/api/v1/sources" || (strings.HasPrefix(r.URL.Path, "/api/v1/sources/") && (strings.HasSuffix(r.URL.Path, "/stop") || r.Method == http.MethodDelete && strings.Count(r.URL.Path, "/") == 4))
			if sourceMutation {
				contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if contentType != "application/json" || r.Header.Get("X-Streamline-Request") != "1" {
					writeError(w, 415, &query.APIError{Code: "invalid_content_type", Message: "source operations require application/json and X-Streamline-Request: 1"})
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func validOrigin(origin *url.URL, raw string) bool {
	return (origin.Scheme == "http" || origin.Scheme == "https") && origin.Host != "" &&
		origin.Path == "" && origin.User == nil && origin.RawQuery == "" &&
		!origin.ForceQuery && !strings.Contains(raw, "#")
}

func matchesTrustedOrigin(origin *url.URL, trusted string) bool {
	if origin.String() == trusted {
		return true
	}
	pattern, err := url.Parse(trusted)
	if err != nil || !validOrigin(pattern, trusted) || pattern.Scheme != origin.Scheme || pattern.Port() != origin.Port() || !strings.HasPrefix(pattern.Hostname(), "*.") {
		return false
	}
	// Include the dot in the suffix so lookalike domains cannot match.
	suffix := strings.ToLower(strings.TrimPrefix(pattern.Hostname(), "*"))
	hostname := strings.ToLower(origin.Hostname())
	return len(hostname) > len(suffix) && strings.HasSuffix(hostname, suffix)
}
