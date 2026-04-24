package main

import (
	_ "embed"
	"net/http"
)

//go:embed api/openapi.yaml
var openapiSpec []byte

func serveOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Write(openapiSpec)
}
