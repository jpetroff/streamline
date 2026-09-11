package httpapi

import (
	"errors"
	"io"
	"io/fs"
	"net/http"

	"streamline/internal/configuration"
)

// WithConfigurations mounts the file-backed settings API inside the same request protections as sources.
func WithConfigurations(frontend http.Handler, store *configuration.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/configurations", func(w http.ResponseWriter, r *http.Request) {
		list, err := store.List()
		if err != nil {
			configurationError(w, err)
			return
		}
		writeJSON(w, 200, list)
	})
	mux.HandleFunc("GET /api/v1/configurations/{id}", func(w http.ResponseWriter, r *http.Request) {
		entry, err := store.Get(r.PathValue("id"))
		if err != nil {
			configurationError(w, err)
			return
		}
		writeJSON(w, 200, entry)
	})
	mux.HandleFunc("DELETE /api/v1/configurations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Delete(r.PathValue("id")); err != nil {
			configurationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	save := func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, configuration.MaxBytes))
		if err != nil {
			configurationError(w, &configuration.ValidationError{Issues: []configuration.Issue{{Field: "document", Message: "Configuration must be at most 64 KiB."}}})
			return
		}
		doc, err := configuration.Decode(data)
		if err != nil {
			configurationError(w, err)
			return
		}
		if r.URL.Path == "/api/v1/configurations/validate" {
			writeJSON(w, 200, doc)
			return
		}
		entry, err := store.Save(r.PathValue("id"), doc)
		if err != nil {
			configurationError(w, err)
			return
		}
		status := http.StatusOK
		if r.Method == http.MethodPost {
			status = http.StatusCreated
		}
		writeJSON(w, status, entry)
	}
	mux.HandleFunc("POST /api/v1/configurations", save)
	mux.HandleFunc("PUT /api/v1/configurations/{id}", save)
	mux.HandleFunc("POST /api/v1/configurations/validate", save)
	mux.Handle("/api/v1/configurations/", http.NotFoundHandler())
	mux.Handle("/", frontend)
	return mux
}
func configurationError(w http.ResponseWriter, err error) {
	status, code, message := 500, "configuration_storage_error", "Could not access saved configurations: "+err.Error()
	var invalid *configuration.ValidationError
	var issues []configuration.Issue
	if errors.As(err, &invalid) {
		status, code, message, issues = 400, "invalid_configuration", invalid.Error(), invalid.Issues
	} else if errors.Is(err, fs.ErrNotExist) {
		status, code, message = 404, "configuration_not_found", "Saved configuration no longer exists."
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "configurationErrors": issues}})
}
