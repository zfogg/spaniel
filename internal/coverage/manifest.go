package coverage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

// LoadManifest parses an OpenAPI 3.x spec file (JSON or YAML) and returns
// every (method, path) it declares. The service the spec belongs to is taken
// from the spec's `info.title` if present, otherwise from the bare filename.
//
// We don't try to enforce strict OpenAPI compliance — just walk paths and
// pull out the HTTP-method keys. Anything we don't recognize is ignored.
func LoadManifest(path string) (*Manifests, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var spec struct {
		Info struct {
			Title string `yaml:"title"`
		} `yaml:"info"`
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	service := spec.Info.Title
	if service == "" {
		service = filepath.Base(path)
		service = strings.TrimSuffix(service, filepath.Ext(service))
	}

	httpMethods := map[string]bool{
		"get": true, "post": true, "put": true, "patch": true, "delete": true,
		"head": true, "options": true,
	}

	var routes []ManifestRoute
	for p, ops := range spec.Paths {
		for method := range ops {
			lm := strings.ToLower(method)
			if !httpMethods[lm] {
				continue
			}
			routes = append(routes, ManifestRoute{
				Method: strings.ToUpper(method),
				Path:   p,
			})
		}
	}

	return &Manifests{
		Spec:   filepath.Base(path),
		Routes: map[string][]ManifestRoute{service: routes},
	}, nil
}

// ParseOpenAPI validates OpenAPI 3.x JSON or YAML and extracts HTTP templates.
// The caller supplies the telemetry service deliberately; info.title is not a
// service identity and is retained only as human-readable metadata elsewhere.
func ParseOpenAPI(raw []byte, service string) ([]ManifestRoute, string, error) {
	if strings.TrimSpace(service) == "" {
		return nil, "", fmt.Errorf("service mapping is required")
	}
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromData(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parse OpenAPI: %w", err)
	}
	// Coverage needs a declared operation inventory, not executable request and
	// response examples. Some otherwise useful generated documents (including
	// Spaniel's own) intentionally use abbreviated examples that fail the full
	// OpenAPI example validator, so validate the structural contract we consume.
	if !strings.HasPrefix(doc.OpenAPI, "3.") || doc.Paths == nil {
		return nil, "", fmt.Errorf("validate OpenAPI: expected an OpenAPI 3 document with paths")
	}
	methods := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true}
	routes := make([]ManifestRoute, 0)
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			upper := strings.ToUpper(method)
			if methods[upper] {
				routes = append(routes, ManifestRoute{Method: upper, Path: normalizePath(path)})
			}
		}
	}
	if len(routes) == 0 {
		return nil, "", fmt.Errorf("OpenAPI document declares no HTTP operations")
	}
	return routes, doc.Info.Title, nil
}

func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return "/"
	}
	return path
}
