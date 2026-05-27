package coverage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
