// Command openapiyaml emits the YAML twin of Spaniel's checked-in public API
// contract. Coverage accepts JSON and YAML, so keeping both generated from the
// same source makes self-coverage a meaningful compatibility check.
package main

import (
	"encoding/json"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	b, err := os.ReadFile("api/openapi.json")
	must(err)
	var document any
	must(json.Unmarshal(b, &document))
	y, err := yaml.Marshal(document)
	must(err)
	must(os.WriteFile("api/openapi.yaml", y, 0o644))
}

func must(err error) { if err != nil { panic(err) } }
