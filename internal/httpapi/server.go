// Package httpapi owns the localhost HTTP routes.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"streamline/internal/query"
)

type handler struct{ queries query.Service }

// NewHandler wires the versioned transport routes to an injected query service and frontend.
func NewHandler(frontend http.Handler, services ...query.Service) http.Handler {
	var service query.Service
	if len(services) > 0 {
		service = services[0]
	}
	if service == nil {
		service = query.NewMemoryService(nil)
	}
	h := handler{queries: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, struct {
			Status string `json:"status"`
		}{Status: "ok"})
	})
	mux.HandleFunc("GET /api/v1/session", h.session)
	mux.HandleFunc("POST /api/v1/queries", h.createQuery)
	mux.HandleFunc("GET /api/v1/queries/{id}", h.getQuery)
	mux.HandleFunc("DELETE /api/v1/queries/{id}", h.deleteQuery)
	mux.HandleFunc("GET /api/v1/queries/{id}/rows", h.queryRows)
	mux.HandleFunc("GET /api/v1/queries/{id}/events", h.queryEvents)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/api", http.NotFoundHandler())
	mux.Handle("/", frontend)
	return mux
}

// session returns the identity and input status used to validate query snapshots.
func (h handler) session(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.queries.Session(r.Context()))
}

// createQuery validates a query command and starts its asynchronous initial scan.
func (h handler) createQuery(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var request query.CreateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, &query.APIError{Code: "invalid_json", Message: "request body must be valid query JSON"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, &query.APIError{Code: "invalid_json", Message: "request body must contain one JSON value"})
		return
	}
	state, err := h.queries.Create(r.Context(), request)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/queries/"+state.QueryID)
	writeJSON(w, http.StatusAccepted, state)
}

// getQuery returns authoritative state for reconnect and recovery.
func (h handler) getQuery(w http.ResponseWriter, r *http.Request) {
	state, err := h.queries.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// deleteQuery releases query indexes without affecting captured records.
func (h handler) deleteQuery(w http.ResponseWriter, r *http.Request) {
	if err := h.queries.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// queryRows reads one bounded window from an immutable snapshot boundary.
func (h handler) queryRows(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("snapshot")
	if token == "" {
		writeError(w, http.StatusBadRequest, &query.APIError{Code: "snapshot_required", Message: "snapshot is required"})
		return
	}
	offset, err := parseUint(r.URL.Query().Get("offset"), 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, &query.APIError{Code: "invalid_offset", Message: "offset must be a non-negative integer"})
		return
	}
	limit64, err := parseUint(r.URL.Query().Get("limit"), query.DefaultPageSize)
	if err != nil || limit64 == 0 || limit64 > query.MaxPageSize {
		writeError(w, http.StatusBadRequest, &query.APIError{Code: "invalid_limit", Message: "limit must be between 1 and 1000"})
		return
	}
	page, err := h.queries.Page(r.Context(), r.PathValue("id"), token, offset, int(limit64))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// queryEvents streams state notifications and heartbeats; row data stays on HTTP pages.
func (h handler) queryEvents(w http.ResponseWriter, r *http.Request) {
	state, session, subscription, err := h.queries.Subscribe(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	defer subscription.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(w)
	if err := writeEvent(controller, w, query.Event{Type: "state", State: state, Session: session}); err != nil {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-subscription.Events():
			if !ok {
				return
			}
			if err := writeEvent(controller, w, event); err != nil {
				return
			}
		case <-heartbeat.C:
			_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}

// writeEvent emits and flushes one named SSE event with a bounded write deadline.
func writeEvent(controller *http.ResponseController, w http.ResponseWriter, event query.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
		return err
	}
	return controller.Flush()
}

// parseUint parses non-negative decimal query parameters, applying a missing-value default.
func parseUint(value string, fallback uint64) (uint64, error) {
	if value == "" {
		return fallback, nil
	}
	if strings.HasPrefix(value, "-") {
		return 0, errors.New("negative")
	}
	return strconv.ParseUint(value, 10, 64)
}

// writeServiceError maps stable domain error codes to HTTP statuses.
func writeServiceError(w http.ResponseWriter, err error) {
	apiErr := query.AsAPIError(err)
	status := http.StatusBadRequest
	switch apiErr.Code {
	case query.ErrNotFound.Code:
		status = http.StatusNotFound
	case query.ErrQueryEngine.Code:
		status = http.StatusNotImplemented
	case "internal_error":
		status = http.StatusInternalServerError
	}
	writeError(w, status, apiErr)
}

// writeError writes the common structured error envelope.
func writeError(w http.ResponseWriter, status int, apiErr *query.APIError) {
	writeJSON(w, status, struct {
		Error *query.APIError `json:"error"`
	}{apiErr})
}

// writeJSON sends a non-cacheable JSON response.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
