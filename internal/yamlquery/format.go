// Package yamlquery formats SQL fields in portable dashboard and alert files.
package yamlquery

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/zfogg/spaniel/internal/sqlformat"
	"gopkg.in/yaml.v3"
)

// Format preserves comments and non-SQL values. Already formatted documents are
// returned byte for byte, avoiding repeated rewrites on directory reloads.
func Format(data []byte) ([]byte, bool, error) {
	var doc yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&doc); err != nil {
		return nil, false, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, false, fmt.Errorf("expected one YAML document")
	}
	changed := false
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i < len(n.Content); i += 2 {
				key, value := n.Content[i].Value, n.Content[i+1]
				if key == "annotations" {
					continue
				}
				sql := key == "query" || key == "annotation_query"
				if key == "source" {
					fields := strings.Fields(value.Value)
					sql = len(fields) > 0 && (strings.EqualFold(fields[0], "select") || strings.EqualFold(fields[0], "with"))
				}
				if sql && value.Kind == yaml.ScalarNode && value.Tag == "!!str" {
					formatted := sqlformat.Format(value.Value)
					if strings.TrimSpace(value.Value) != strings.TrimSpace(formatted) {
						value.Value = strings.TrimSpace(formatted) + "\n"
						value.Style = yaml.LiteralStyle
						changed = true
					}
				} else {
					walk(value)
				}
			}
		} else {
			for _, child := range n.Content {
				walk(child)
			}
		}
	}
	walk(&doc)
	if !changed {
		return data, false, nil
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return nil, false, err
	}
	if err := encoder.Close(); err != nil {
		return nil, false, err
	}
	return out.Bytes(), true, nil
}
