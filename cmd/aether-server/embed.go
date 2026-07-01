package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:frontend-dist
var frontendAssets embed.FS

func frontendHandler() http.Handler {
	sub, err := fs.Sub(frontendAssets, "frontend-dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" {
			_, err := fs.Stat(sub, path[1:])
			if err != nil {
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
