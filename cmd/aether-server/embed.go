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
	fileServer := http.FileServer(http.FS(sub))

	cfgJSON, _ := json.Marshal(cfg)
	injectTag := []byte(`<script>window.__AETHER_CONFIG__=` + string(cfgJSON) + `</script>`)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if path == "/" || path == "/index.html" {
			idx, err := fs.ReadFile(sub, "index.html")
			if err == nil {
				idx = bytes.Replace(idx, []byte("</head>"), append(injectTag, []byte("</head>")...), 1)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-cache")
				w.Write(idx)
				return
			}
		}

		if path != "/" {
			if _, err := fs.Stat(sub, path[1:]); err != nil {
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
