package server

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
)

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; " +
	"base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

var hashedAsset = regexp.MustCompile(`-[A-Z0-9]{8}\.(js|css|woff2?|ttf|svg|png)$`)

type site struct {
	files fs.FS
	serve http.Handler
}

func Site(dir string) (http.Handler, error) {
	files := os.DirFS(dir)
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return nil, fmt.Errorf("web UI: %w", err)
	}
	return &site{files: files, serve: http.FileServerFS(files)}, nil
}

func (s *site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "index.html" || !s.exists(name) {
		if path.Ext(name) != "" && name != "index.html" {
			http.NotFound(w, r)
			return
		}
		index, err := fs.ReadFile(s.files, "index.html")
		if err != nil {
			http.Error(w, "the web UI is not built", http.StatusServiceUnavailable)
			return
		}
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
		return
	}

	if hashedAsset.MatchString(name) {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	s.serve.ServeHTTP(w, r)
}

func (s *site) exists(name string) bool {
	info, err := fs.Stat(s.files, name)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
