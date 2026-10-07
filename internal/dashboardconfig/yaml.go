// Package dashboardconfig converts portable dashboard definitions to and from
// Spaniel's persisted dashboard records. Definitions intentionally omit
// database IDs, timestamps, and query results.
package dashboardconfig

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zfogg/spaniel/internal/model"
	"gopkg.in/yaml.v3"
)

const Version = 1

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

var MagicVariables = []MagicVar{
	{Name: "service_name", Kind: "string"}, {Name: "environment", Kind: "string"},
	{Name: "window", Kind: "time"}, {Name: "operation_name", Kind: "string"},
	{Name: "status", Kind: "string"}, {Name: "selected_trace_id", Kind: "trace_id"},
}

func FromDashboard(d *model.Dashboard) (Definition, error) {
	out := Definition{Version: Version, Name: d.Name, Description: d.Description, MagicVariables: MagicVariables}
	for _, v := range d.Variables {
		var options []string
		if err := yaml.Unmarshal([]byte(v.OptionsJSON), &options); err != nil {
			return Definition{}, fmt.Errorf("variable %q options: %w", v.Name, err)
		}
		out.Variables = append(out.Variables, Variable{Name: v.Name, Kind: v.Kind, Source: v.Source, Options: options, DefaultValue: v.DefaultValue})
	}
	for _, p := range d.Panels {
		var settings, layout any
		if err := yaml.Unmarshal([]byte(p.SettingsJSON), &settings); err != nil {
			return Definition{}, fmt.Errorf("panel %q settings: %w", p.Title, err)
		}
		if err := yaml.Unmarshal([]byte(p.LayoutJSON), &layout); err != nil {
			return Definition{}, fmt.Errorf("panel %q layout: %w", p.Title, err)
		}
		out.Panels = append(out.Panels, Panel{Title: p.Title, DisplayType: p.DisplayType, Query: p.QuerySQL, Settings: settings, Layout: layout, Position: p.Position})
	}
	return out, nil
}

func Parse(data []byte) (Definition, error) {
	var d Definition
	if err := yaml.Unmarshal(data, &d); err != nil {
		return d, err
	}
	if d.Version != Version {
		return d, fmt.Errorf("unsupported dashboard definition version %d", d.Version)
	}
	if strings.TrimSpace(d.Name) == "" {
		return d, fmt.Errorf("dashboard name is required")
	}
	for _, v := range d.Variables {
		if strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Kind) == "" || strings.TrimSpace(v.Source) == "" {
			return d, fmt.Errorf("each variable needs name, kind, and source")
		}
	}
	for _, p := range d.Panels {
		if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.DisplayType) == "" || strings.TrimSpace(p.Query) == "" {
			return d, fmt.Errorf("each panel needs title, display_type, and query")
		}
	}
	return d, nil
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
