package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
)

//go:embed all:frontend-dist
var frontendAssets embed.FS

type runtimeConfig struct {
	APIURL   string `json:"apiUrl,omitempty"`
	RelayURL string `json:"relayUrl,omitempty"`
}

func frontendHandler(cfg *runtimeConfig) http.Handler {
	sub, err := fs.Sub(frontendAssets, "frontend-dist")
	if err != nil {
		panic(err)
	}
	return frontendHandlerWithFS(sub, cfg)
}

func frontendHandlerWithFS(assets fs.FS, cfg *runtimeConfig) http.Handler {
	fileServer := http.FileServer(http.FS(assets))

	cfgJSON, _ := json.Marshal(cfg)
	injectTag := []byte(`<script>window.__AETHER_CONFIG__=` + string(cfgJSON) + `</script>`)

	idxBytes, err := fs.ReadFile(assets, "index.html")
	idxInjected := []byte{}
	if err == nil {
		idxInjected = bytes.Replace(idxBytes, []byte("</head>"), append(injectTag, []byte("</head>")...), 1)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Serve static assets (JS, CSS, images, fonts) directly
		if path != "/" && path != "/index.html" {
			if _, err := fs.Stat(assets, path[1:]); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Serve index.html with config injection.
		// This covers root path ("/"), "/index.html", and all SPA fallback
		// routes ("/notebooks/<id>", "/dashboards", etc.).
		if len(idxInjected) > 0 {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(idxInjected)
		} else {
			fileServer.ServeHTTP(w, r)
		}
	})
}
