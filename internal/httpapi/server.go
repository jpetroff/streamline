// Package httpapi owns the localhost HTTP routes.
package httpapi

import (
	"encoding/json"
	"net/http"
)

func NewHandler(frontend http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Status string `json:"status"`
		}{Status: "ok"})
	})
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/api", http.NotFoundHandler())
	mux.Handle("/", frontend)
	return mux
}
