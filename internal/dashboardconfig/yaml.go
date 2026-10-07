// Package dashboardconfig converts portable dashboard definitions to and from
// Spaniel's persisted dashboard records. Definitions intentionally omit
// database IDs, timestamps, and query results.
package dashboardconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zfogg/spaniel/internal/model"
	"gopkg.in/yaml.v3"
)

const Version = 1

const (
	maxDashboardNameLength        = 120
	maxDashboardDescriptionLength = 1000
	maxVariableNameLength         = 64
	maxVariableSourceLength       = 16000
	maxPanelTitleLength           = 160
	maxPanelQueryLength           = 16000
)

var (
	variableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	variableKinds       = map[string]struct{}{"attribute": {}, "string": {}, "number": {}, "boolean": {}, "duration": {}, "time": {}, "enum": {}, "service": {}, "operation": {}, "trace_id": {}, "span_id": {}, "log_id": {}}
	displayTypes        = map[string]struct{}{"single_value": {}, "time_series": {}, "table": {}, "heatmap": {}, "entity_list": {}, "trace_list": {}, "span_list": {}, "log_list": {}, "deploy_correlation": {}}
)

type Definition struct {
	Version        int        `yaml:"version"`
	Name           string     `yaml:"name"`
	Description    string     `yaml:"description,omitempty"`
	MagicVariables []MagicVar `yaml:"magic_variables,omitempty"`
	Variables      []Variable `yaml:"variables,omitempty"`
	Panels         []Panel    `yaml:"panels,omitempty"`
}

type MagicVar struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
}

type Variable struct {
	Name         string   `yaml:"name"`
	Kind         string   `yaml:"kind"`
	Source       string   `yaml:"source"`
	Options      []string `yaml:"options,omitempty"`
	DefaultValue string   `yaml:"default_value,omitempty"`
}

type Panel struct {
	Title       string `yaml:"title"`
	DisplayType string `yaml:"display_type"`
	Query       string `yaml:"query"`
	Settings    any    `yaml:"settings,omitempty"`
	Layout      any    `yaml:"layout,omitempty"`
	Position    int    `yaml:"position"`
}

// MagicVariables are reserved values owned by Spaniel. Dashboard authors can
// define every other filter explicitly, so their source/options/default remain
// portable and visible in YAML. The active session is the only universally
// available automatic context today.
var MagicVariables = []MagicVar{{Name: "session_id", Kind: "string"}}

func FromDashboard(d *model.Dashboard) (Definition, error) {
	out := Definition{Version: Version, Name: d.Name, Description: d.Description, MagicVariables: MagicVariables}
	for _, v := range d.Variables {
		var options []string
		if err := yaml.Unmarshal([]byte(v.OptionsJSON), &options); err != nil {
			return Definition{}, fmt.Errorf("variable %q options: %w", v.Name, err)
		}
		out.Variables = append(out.Variables, Variable{Name: v.Name, Kind: v.Kind, Source: v.Source, Options: options, DefaultValue: v.DefaultValue})
	}
	panels := append([]*model.DashboardPanel(nil), d.Panels...)
	sort.SliceStable(panels, func(i, j int) bool { return panels[i].Position < panels[j].Position })
	for i, p := range panels {
		var settings, layout any
		if err := yaml.Unmarshal([]byte(p.SettingsJSON), &settings); err != nil {
			return Definition{}, fmt.Errorf("panel %q settings: %w", p.Title, err)
		}
		if err := yaml.Unmarshal([]byte(p.LayoutJSON), &layout); err != nil {
			return Definition{}, fmt.Errorf("panel %q layout: %w", p.Title, err)
		}
		// Persisted dashboards predate portable definitions and may have gaps or
		// duplicate positions. Export a canonical sequence that can be imported.
		out.Panels = append(out.Panels, Panel{Title: p.Title, DisplayType: p.DisplayType, Query: p.QuerySQL, Settings: settings, Layout: layout, Position: i})
	}
	return out, nil
}

func Parse(data []byte) (Definition, error) {
	var d Definition
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&d); err != nil {
		return d, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return d, fmt.Errorf("dashboard YAML must contain exactly one document")
		}
		return d, err
	}
	if d.Version != Version {
		return d, fmt.Errorf("unsupported dashboard definition version %d", d.Version)
	}
	if strings.TrimSpace(d.Name) == "" || len(d.Name) > maxDashboardNameLength {
		if len(d.Name) > maxDashboardNameLength {
			return d, fmt.Errorf("dashboard name must be at most %d characters", maxDashboardNameLength)
		}
		return d, fmt.Errorf("dashboard name is required")
	}
	if len(d.Description) > maxDashboardDescriptionLength {
		return d, fmt.Errorf("dashboard description must be at most %d characters", maxDashboardDescriptionLength)
	}
	if err := validateMagicVariables(d.MagicVariables); err != nil {
		return d, err
	}
	if err := validateVariables(d.Variables); err != nil {
		return d, err
	}
	if err := validatePanels(d.Panels); err != nil {
		return d, err
	}
	return d, nil
}

func validateMagicVariables(vars []MagicVar) error {
	known := make(map[string]string, len(MagicVariables))
	for _, variable := range MagicVariables {
		known[variable.Name] = variable.Kind
	}
	seen := map[string]struct{}{}
	for i, variable := range vars {
		if expectedKind, ok := known[variable.Name]; !ok || variable.Kind != expectedKind {
			return fmt.Errorf("magic_variables[%d] is not a supported magic variable", i)
		}
		if _, ok := seen[variable.Name]; ok {
			return fmt.Errorf("magic_variables[%d] duplicates %q", i, variable.Name)
		}
		seen[variable.Name] = struct{}{}
	}
	return nil
}

func validateVariables(vars []Variable) error {
	seen := map[string]struct{}{}
	magic := map[string]struct{}{}
	for _, variable := range MagicVariables {
		magic[variable.Name] = struct{}{}
	}
	for i, variable := range vars {
		name := strings.TrimPrefix(variable.Name, "$")
		if name == "" || len(name) > maxVariableNameLength || !variableNamePattern.MatchString(name) {
			return fmt.Errorf("variables[%d].name must be a %d-character SQL parameter name", i, maxVariableNameLength)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("variables[%d].name duplicates %q", i, name)
		}
		if _, ok := magic[name]; ok {
			return fmt.Errorf("variables[%d].name %q conflicts with a magic variable", i, name)
		}
		seen[name] = struct{}{}
		if _, ok := variableKinds[variable.Kind]; !ok {
			return fmt.Errorf("variables[%d].kind %q is not supported", i, variable.Kind)
		}
		if strings.TrimSpace(variable.Source) == "" || len(variable.Source) > maxVariableSourceLength {
			return fmt.Errorf("variables[%d].source is required and must be at most %d characters", i, maxVariableSourceLength)
		}
		if err := validateVariableDefault(i, variable); err != nil {
			return err
		}
	}
	return nil
}

func validateVariableDefault(index int, variable Variable) error {
	if variable.DefaultValue == "" {
		return nil
	}
	switch variable.Kind {
	case "number":
		if _, err := strconv.ParseFloat(variable.DefaultValue, 64); err != nil {
			return fmt.Errorf("variables[%d].default_value must be a number", index)
		}
	case "boolean":
		if _, err := strconv.ParseBool(variable.DefaultValue); err != nil {
			return fmt.Errorf("variables[%d].default_value must be true or false", index)
		}
	case "duration":
		if _, err := time.ParseDuration(variable.DefaultValue); err != nil {
			return fmt.Errorf("variables[%d].default_value must be a duration such as 500ms or 5m", index)
		}
	case "enum":
		for _, option := range variable.Options {
			if option == variable.DefaultValue {
				return nil
			}
		}
		return fmt.Errorf("variables[%d].default_value must be one of its enum options", index)
	}
	return nil
}

func validatePanels(panels []Panel) error {
	positions := make([]bool, len(panels))
	for i, panel := range panels {
		if strings.TrimSpace(panel.Title) == "" || len(panel.Title) > maxPanelTitleLength {
			return fmt.Errorf("panels[%d].title is required and must be at most %d characters", i, maxPanelTitleLength)
		}
		if _, ok := displayTypes[panel.DisplayType]; !ok {
			return fmt.Errorf("panels[%d].display_type %q is not supported", i, panel.DisplayType)
		}
		if strings.TrimSpace(panel.Query) == "" || len(panel.Query) > maxPanelQueryLength {
			return fmt.Errorf("panels[%d].query is required and must be at most %d characters", i, maxPanelQueryLength)
		}
		if err := validateObject("settings", i, panel.Settings); err != nil {
			return err
		}
		if err := validateObject("layout", i, panel.Layout); err != nil {
			return err
		}
		if panel.Position < 0 || panel.Position >= len(panels) {
			return fmt.Errorf("panels[%d].position must be between 0 and %d", i, len(panels)-1)
		}
		if positions[panel.Position] {
			return fmt.Errorf("panels[%d].position %d is duplicated", i, panel.Position)
		}
		positions[panel.Position] = true
	}
	return nil
}

func validateObject(field string, panelIndex int, value any) error {
	if value == nil {
		return nil
	}
	if _, ok := value.(map[string]any); !ok {
		return fmt.Errorf("panels[%d].%s must be a mapping", panelIndex, field)
	}
	return nil
}

func Marshal(d *model.Dashboard) ([]byte, error) {
	definition, err := FromDashboard(d)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(definition)
}

func (d Definition) Dashboard(id string) (*model.Dashboard, error) {
	out := &model.Dashboard{ID: id, Name: d.Name, Description: d.Description}
	for _, v := range d.Variables {
		options, err := json.Marshal(v.Options)
		if err != nil {
			return nil, err
		}
		out.Variables = append(out.Variables, &model.DashboardVariable{DashboardID: id, Name: strings.TrimPrefix(v.Name, "$"), Kind: v.Kind, Source: v.Source, OptionsJSON: string(options), DefaultValue: v.DefaultValue})
	}
	for _, p := range d.Panels {
		settings, err := json.Marshal(p.Settings)
		if err != nil {
			return nil, err
		}
		layout, err := json.Marshal(p.Layout)
		if err != nil {
			return nil, err
		}
		if p.Settings == nil {
			settings = []byte("{}")
		}
		if p.Layout == nil {
			layout = []byte("{}")
		}
		out.Panels = append(out.Panels, &model.DashboardPanel{DashboardID: id, Title: p.Title, DisplayType: p.DisplayType, QuerySQL: p.Query, QueryVersion: 1, SettingsJSON: string(settings), LayoutJSON: string(layout), Position: p.Position})
	}
	return out, nil
}
