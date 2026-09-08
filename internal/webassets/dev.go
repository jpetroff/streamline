//go:build dev

package webassets

import "net/http"

// Development serves the UI through Vite without requiring built assets.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "The development frontend is at http://localhost:5173", http.StatusNotFound)
	})
}

// TrustedOrigins permits the development frontend proxy, Coder shared ports,
// and browser tests. Vite rewrites the API request host to its localhost target.
func TrustedOrigins() []string {
	return []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
		"http://127.0.0.1:5174",
		"https://*.coder.intranet",
	}
}
