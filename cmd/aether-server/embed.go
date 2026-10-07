package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
)

//go:embed all:frontend-dist
var frontendAssets embed.FS

type runtimeConfig struct {
	APIURL   string `json:"apiUrl,omitempty"`
	RelayURL string `json:"relayUrl,omitempty"`
}

// assetEntry is a static frontend file prepared once at startup: its raw
// bytes, pre-compressed brotli/gzip variants, an ETag, and its content type.
// The compressed bytes are computed once and shared across requests.
type assetEntry struct {
	raw    []byte
	gzip   []byte
	brotli []byte
	etag   string
	ctype  string
}

// compressionSkipExts lists extensions that are already compressed (or not
// worth compressing), so the startup pass leaves them untouched.
var compressionSkipExts = map[string]bool{
	".woff2": true, ".woff": true, ".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".webp": true, ".avif": true, ".ico": true, ".zip": true,
	".gz": true, ".br": true, ".mp4": true, ".webm": true, ".mp3": true,
	".pdf": true,
}

// minCompressSize is the smallest file size worth compressing; below this the
// framing overhead outweighs the savings.
const minCompressSize = 1024

func frontendHandler(cfg *runtimeConfig) http.Handler {
	sub, err := fs.Sub(frontendAssets, "frontend-dist")
	if err != nil {
		panic(err)
	}
	return frontendHandlerWithFS(sub, cfg)
}

// buildAssetEntries walks the embedded frontend and prepares each file for
// serving: content type, ETag, and pre-compressed variants. Doing this once at
// startup keeps request handling cheap.
func buildAssetEntries(assets fs.FS) map[string]*assetEntry {
	entries := make(map[string]*assetEntry)
	err := fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(assets, p)
		if err != nil {
			return err
		}
		e := &assetEntry{raw: data}
		sum := sha256.Sum256(data)
		e.etag = `"` + hex.EncodeToString(sum[:8]) + `"`
		ext := strings.ToLower(path.Ext(p))
		if ct := mime.TypeByExtension(ext); ct != "" {
			e.ctype = ct
		} else {
			e.ctype = http.DetectContentType(data)
		}
		if len(data) >= minCompressSize && !compressionSkipExts[ext] {
			if gz := gzipBytes(data); len(gz) > 0 && len(gz) < len(data) {
				e.gzip = gz
			}
			if br := brotliBytes(data); len(br) > 0 && len(br) < len(data) {
				e.brotli = br
			}
		}
		entries[path.Clean(p)] = e
		return nil
	})
	if err != nil {
		panic(err)
	}
	return entries
}

func gzipBytes(data []byte) []byte {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		return nil
	}
	if _, err := w.Write(data); err != nil {
		return nil
	}
	if err := w.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}

func brotliBytes(data []byte) []byte {
	var buf bytes.Buffer
	w := brotli.NewWriterLevel(&buf, 5)
	if _, err := w.Write(data); err != nil {
		return nil
	}
	if err := w.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}

// serve writes the asset with cache validators, an immutable cache policy for
// content-hashed bundles, and the best accepted compression variant.
func (e *assetEntry) serve(w http.ResponseWriter, r *http.Request, name string) {
	h := w.Header()
	h.Set("ETag", e.etag)
	if e.gzip != nil || e.brotli != nil {
		h.Set("Vary", "Accept-Encoding")
	}
	if etagMatches(r.Header.Get("If-None-Match"), e.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if e.ctype != "" {
		h.Set("Content-Type", e.ctype)
	}
	if strings.HasPrefix(name, "assets/") {
		// Vite emits content-hashed filenames under assets/, so they can be
		// cached forever.
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}

	data := e.raw
	if r.Header.Get("Range") == "" {
		// Ranges are served from the identity representation to keep
		// Content-Range semantics simple; browsers only range media/fonts,
		// which are in the skip list anyway.
		ae := r.Header.Get("Accept-Encoding")
		qBr := encodingQuality(ae, "br")
		qGz := encodingQuality(ae, "gzip")
		switch {
		case e.brotli != nil && qBr > 0 && qBr >= qGz:
			h.Set("Content-Encoding", "br")
			data = e.brotli
		case e.gzip != nil && qGz > 0:
			h.Set("Content-Encoding", "gzip")
			data = e.gzip
		}
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func frontendHandlerWithFS(assets fs.FS, cfg *runtimeConfig) http.Handler {
	entries := buildAssetEntries(assets)

	cfgJSON, _ := json.Marshal(cfg)
	injectTag := []byte(`<script>window.__AETHER_CONFIG__=` + string(cfgJSON) + `</script>`)

	idxBytes, err := fs.ReadFile(assets, "index.html")
	idxInjected := []byte{}
	idxETag := ""
	if err == nil {
		idxInjected = bytes.Replace(idxBytes, []byte("</head>"), append(injectTag, []byte("</head>")...), 1)
		sum := sha256.Sum256(idxInjected)
		idxETag = `"` + hex.EncodeToString(sum[:8]) + `"`
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")

		// Serve static assets (JS, CSS, images, fonts) directly.
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			if e, ok := entries[path.Clean(name)]; ok {
				e.serve(w, r, name)
				return
			}
		}

		// Serve index.html with config injection.
		// This covers root path ("/"), "/index.html", and all SPA fallback
		// routes ("/notebooks/<id>", "/dashboards", etc.).
		if len(idxInjected) > 0 {
			h := w.Header()
			h.Set("Content-Type", "text/html; charset=utf-8")
			h.Set("Cache-Control", "no-cache")
			h.Set("ETag", idxETag)
			if etagMatches(r.Header.Get("If-None-Match"), idxETag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Write(idxInjected)
			return
		}
		http.NotFound(w, r)
	})
}

// etagMatches reports whether an If-None-Match header matches etag. It accepts
// the "*" wildcard and weak ("W/") prefixes.
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || part == etag || strings.TrimPrefix(part, "W/") == etag {
			return true
		}
	}
	return false
}

// encodingQuality returns the q-value (0 when not accepted) for an encoding in
// an Accept-Encoding header.
func encodingQuality(header, enc string) float64 {
	best := 0.0
	found := false
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := part
		q := 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			name = strings.TrimSpace(part[:i])
			for _, param := range strings.Split(part[i+1:], ";") {
				param = strings.TrimSpace(param)
				if strings.HasPrefix(param, "q=") {
					if v, err := strconv.ParseFloat(strings.TrimPrefix(param, "q="), 64); err == nil {
						q = v
					}
				}
			}
		}
		if name == enc {
			found = true
			if q > best {
				best = q
			}
		}
	}
	if !found {
		return 0
	}
	return best
}
