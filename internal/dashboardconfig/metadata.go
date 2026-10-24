package dashboardconfig

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"sync"
)

var metadataWriteMu sync.Mutex

// WriteMetadata preserves the source document, including comments and panel definitions.
// The source hash prevents overwriting a file edited since its last successful load.
func WriteMetadata(path, id, hash, name, description string) error {
	metadataWriteMu.Lock()
	defer metadataWriteMu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != hash {
		return fmt.Errorf("dashboard YAML changed; reload the dashboard before saving")
	}
	definition, err := Parse(data)
	if err != nil {
		return err
	}
	if definition.ID != id {
		return fmt.Errorf("dashboard YAML id no longer matches")
	}
	definition.Name, definition.Description = name, description
	if _, err := definition.Dashboard(id); err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	mapping := document.Content[0]
	for key, value := range map[string]string{"name": name, "description": description} {
		var node *yaml.Node
		for i := 0; i < len(mapping.Content); i += 2 {
			if mapping.Content[i].Value == key {
				node = mapping.Content[i+1]
				break
			}
		}
		if node == nil {
			node = &yaml.Node{}
			mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, node)
		}
		node.Kind, node.Tag, node.Value, node.Style = yaml.ScalarNode, "!!str", value, 0
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".spaniel-dashboard-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := file.Write(output.Bytes()); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
