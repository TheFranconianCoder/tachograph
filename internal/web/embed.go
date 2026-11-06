// Package web embeds and serves the PWA from the same origin as the API.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Asset reads one file from the embedded PWA build. It lets the app reuse the
// manifest name for PWA discovery without a second copy of the string.
func Asset(name string) ([]byte, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(sub, name)
}

// Handler serves the built PWA. Unknown paths without a file extension fall
// back to index.html so a client-side route survives a reload; missing assets
// are an honest 404.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}

	index, indexErr := fs.ReadFile(sub, "index.html")
	if indexErr != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(notBuiltPage))
		})
	}

	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			files.ServeHTTP(w, r)
			return
		}

		name := strings.TrimPrefix(r.URL.Path, "/")
		switch {
		case name == "":
			serveHTML(w, index)
		case exists(sub, name):
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			if strings.HasSuffix(name, ".webmanifest") {
				w.Header().Set("Content-Type", "application/manifest+json")
			}
			files.ServeHTTP(w, r)
		case strings.Contains(lastSegment(name), "."):
			http.NotFound(w, r)
		default:
			serveHTML(w, index)
		}
	})
}

func exists(sub fs.FS, name string) bool {
	f, err := sub.Open(name)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func lastSegment(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func serveHTML(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

const notBuiltPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Tachograph</title></head>
<body style="font-family:system-ui;margin:3rem;color:#111">
<h1>UI not built</h1>
<p>The PWA assets are missing from this binary. Build them with <code>task web:build</code>
(or <code>cd web &amp;&amp; pnpm install &amp;&amp; pnpm run build</code>) and rebuild the binary.</p>
</body></html>`
