package api

import (
	"embed"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed dist
var staticFS embed.FS

// serveStatic mounts the static files embedded from dist/ on the provided ServeMux.
// All requests that are not matching API endpoints will be handled here.
func serveStatic(mux *http.ServeMux) {
	// Strip "dist" prefix from the embedded filesystem
	subFS, err := fs.Sub(staticFS, "dist")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(subFS))

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// If path is API, let standard handlers deal with it
		if strings.HasPrefix(path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Clean the path to avoid directory traversals and standardise it
		cleanedPath := filepath.Clean(path)
		trimmedPath := strings.TrimPrefix(cleanedPath, "/")

		// Default to index.html if accessing root
		if trimmedPath == "" || trimmedPath == "." {
			trimmedPath = "index.html"
		}

		// Open the file inside the subFS to see if it exists
		f, err := subFS.Open(trimmedPath)
		if err != nil {
			// If file does not exist, serve index.html for client-side routing fallback (SPA)
			indexHTML, err := fs.ReadFile(subFS, "index.html")
			if err != nil {
				http.Error(w, "Index not found", http.StatusInternalServerError)
				return
			}

			// Ensure client does not cache index.html so frontend updates are immediately visible
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			w.Write(indexHTML)
			return
		}
		f.Close()

		// Serve the static asset normally using standard FileServer
		fileServer.ServeHTTP(w, r)
	})
}
