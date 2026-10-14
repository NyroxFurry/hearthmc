// Package web embeds the compiled web UI (built from /web with Vite) into the binary.
package web

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var dist embed.FS

type asset struct {
	data  []byte
	gz    []byte
	ctype string
}

type Server struct {
	assets map[string]*asset
	start  time.Time
	// ExtraConnect returns additional origins the page may connect to (the
	// panel's map port, so the UI can check that browsers reach it).
	ExtraConnect func(r *http.Request) string
}

var compressible = map[string]bool{".js": true, ".css": true, ".html": true, ".svg": true, ".json": true, ".txt": true, ".map": true, ".ttf": true}

// New loads (and pre-compresses) all embedded assets.
func New() *Server {
	s := &Server{assets: map[string]*asset{}, start: time.Now()}
	sub, _ := fs.Sub(dist, "dist")
	fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(sub, p)
		if err != nil {
			return nil
		}
		ext := path.Ext(p)
		ct := mime.TypeByExtension(ext)
		if ct == "" {
			ct = "application/octet-stream"
		}
		a := &asset{data: b, ctype: ct}
		if compressible[ext] && len(b) > 512 {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(b)
			zw.Close()
			if buf.Len() < len(b) {
				a.gz = buf.Bytes()
			}
		}
		s.assets["/"+p] = a
		return nil
	})
	return s
}

func (s *Server) write(w http.ResponseWriter, r *http.Request, a *asset, cache string) {
	h := w.Header()
	h.Set("Content-Type", a.ctype)
	h.Set("Cache-Control", cache)
	h.Add("Vary", "Accept-Encoding")
	if a.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		w.Write(a.gz)
		return
	}
	w.Write(a.data)
}

// ServeHTTP serves static files (hashed assets are cached forever).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assets[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	cache := "public, max-age=3600"
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		cache = "public, max-age=31536000, immutable"
	}
	s.write(w, r, a, cache)
}

// Index serves the SPA entry for the panel ("panel") or the public pages ("public"),
// injecting meta tags into <!--HEARTH_META-->.
func (s *Server) Index(w http.ResponseWriter, r *http.Request, page, meta string) {
	file := "/index.html"
	if page == "public" {
		file = "/public.html"
	}
	a, ok := s.assets[file]
	if !ok {
		a = s.assets["/index.html"]
	}
	if a == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<h1>Hearth</h1><p>Die Weboberfläche wurde nicht mitgebaut. Bitte <code>make web</code> ausführen.</p>"))
		return
	}
	connect := "'self' ws: wss:"
	if s.ExtraConnect != nil {
		if extra := s.ExtraConnect(r); extra != "" && !strings.ContainsAny(extra, ";,\r\n'") {
			connect += " " + extra
		}
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob: https:; style-src 'self' 'unsafe-inline'; "+
		"script-src 'self'; connect-src "+connect+"; font-src 'self' data:; frame-src https: http:; object-src 'none'; base-uri 'self'")
	if meta == "" {
		s.write(w, r, a, "no-cache")
		return
	}
	html := a.data
	if strings.Contains(meta, "<title>") {
		// drop the default title of the template, the injected one replaces it
		if i := bytes.Index(html, []byte("<title>")); i >= 0 {
			if j := bytes.Index(html[i:], []byte("</title>")); j >= 0 {
				html = append(append([]byte{}, html[:i]...), html[i+j+len("</title>"):]...)
			}
		}
	}
	if strings.Contains(meta, `name="theme-color"`) {
		// the injected color (the page's flavor) replaces the default one
		if i := bytes.Index(html, []byte(`<meta name="theme-color"`)); i >= 0 {
			if j := bytes.IndexByte(html[i:], '>'); j >= 0 {
				html = append(append([]byte{}, html[:i]...), html[i+j+1:]...)
			}
		}
	}
	html = bytes.Replace(html, []byte("<!--HEARTH_META-->"), []byte(meta), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(html)
}
