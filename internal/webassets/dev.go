//go:build dev

package webassets

import "net/http"

// Development serves the UI through Vite without requiring built assets.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "The development frontend is at http://localhost:5173", http.StatusNotFound)
	})
}
