// Package alertconfig defines portable, strictly validated alert rule YAML.
package alertconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage"
	"gopkg.in/yaml.v3"
	"io"
	"strings"
	"time"
)

const Version = 1

type Definition struct {
	Version         int               `yaml:"version"`
	ID              string            `yaml:"id,omitempty"`
	Name            string            `yaml:"name"`
	Query           string            `yaml:"query"`
	Condition       Condition         `yaml:"condition"`
	GroupBy         []string          `yaml:"group_by,omitempty"`
	PendingFor      string            `yaml:"pending_for"`
	Cooldown        string            `yaml:"cooldown"`
	RepeatInterval  string            `yaml:"repeat_interval,omitempty"`
	Severity        string            `yaml:"severity"`
	Enabled         *bool             `yaml:"enabled,omitempty"`
	BrowserEnabled  *bool             `yaml:"browser_enabled,omitempty"`
	PushoverEnabled *bool             `yaml:"pushover_enabled,omitempty"`
	InstanceDiscovery *InstanceDiscovery `yaml:"instance_discovery,omitempty"`
	Annotations     map[string]string `yaml:"annotations,omitempty"`
}
type InstanceDiscovery struct {
	Query      string `yaml:"query"`
	Every      string `yaml:"every,omitempty"`
	StaleAfter string `yaml:"stale_after,omitempty"`
}
type Condition struct {
	Kind      string   `yaml:"kind,omitempty"`
	Operator  string   `yaml:"operator,omitempty"`
	Threshold *float64 `yaml:"threshold,omitempty"`
	Pattern   string   `yaml:"pattern,omitempty"`
	RuleIDs   []string `yaml:"rule_ids,omitempty"`
	// Value is accepted only for YAML emitted before threshold was named
	// explicitly. New exports always use threshold.
	Value *float64 `yaml:"value,omitempty"`
}

func Parse(data []byte) (Definition, error) {
	var d Definition
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return d, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return d, fmt.Errorf("alert YAML must contain exactly one document")
	}
	if d.Version != Version {
		return d, fmt.Errorf("unsupported alert definition version %d", d.Version)
	}
	if strings.TrimSpace(d.ID) != d.ID || len(d.ID) > 128 {
		return d, fmt.Errorf("id must not contain surrounding whitespace and must be at most 128 characters")
	}
	if strings.TrimSpace(d.Name) == "" || len(d.Name) > 120 {
		return d, fmt.Errorf("alert name is required and must be at most 120 characters")
	}
	if strings.TrimSpace(d.Query) == "" || len(d.Query) > 16000 {
		return d, fmt.Errorf("alert query is required and must be at most 16000 characters")
	}
	kind := d.Condition.Kind
	if kind == "" {
		kind = "threshold"
	}
	switch kind {
	case "threshold", "count", "no_data", "log_match", "any_of", "all_of":
	default:
		return d, fmt.Errorf("condition.kind must be threshold, count, no_data, log_match, any_of, or all_of")
	}
	if kind == "log_match" {
		if strings.TrimSpace(d.Condition.Pattern) == "" {
			return d, fmt.Errorf("log_match condition.pattern is required")
		}
		if d.Condition.Operator != "" {
			switch d.Condition.Operator {
			case ">", ">=", "<", "<=", "=", "!=":
			default:
				return d, fmt.Errorf("log_match condition.operator is invalid")
			}
			if d.Condition.Threshold == nil && d.Condition.Value == nil {
				return d, fmt.Errorf("log_match condition.threshold is required when operator is set")
			}
		} else if d.Condition.Threshold != nil || d.Condition.Value != nil {
			return d, fmt.Errorf("log_match condition.operator is required when threshold is set")
		}
		if d.Condition.Threshold != nil && d.Condition.Value != nil {
			return d, fmt.Errorf("log_match condition must contain only one of threshold or value")
		}
	} else if kind == "any_of" || kind == "all_of" {
		if len(d.Condition.RuleIDs) == 0 {
			return d, fmt.Errorf("%s condition.rule_ids is required", kind)
		}
		if d.Condition.Threshold != nil || d.Condition.Value != nil || d.Condition.Operator != "" || d.Condition.Pattern != "" {
			return d, fmt.Errorf("%s condition can contain only rule_ids", kind)
		}
	} else if kind != "no_data" {
		switch d.Condition.Operator {
		case ">", ">=", "<", "<=", "=", "!=":
		default:
			return d, fmt.Errorf("condition.operator is invalid")
		}
		if d.Condition.Threshold == nil && d.Condition.Value == nil {
			return d, fmt.Errorf("condition.threshold is required")
		}
		if d.Condition.Threshold != nil && d.Condition.Value != nil {
			return d, fmt.Errorf("condition must contain only one of threshold or value")
		}
	} else if d.Condition.Threshold != nil || d.Condition.Value != nil || d.Condition.Operator != "" {
		return d, fmt.Errorf("no_data condition cannot include operator or threshold")
	}
	switch d.Severity {
	case "info", "warning", "critical":
	default:
		return d, fmt.Errorf("severity must be info, warning, or critical")
	}
	if d.InstanceDiscovery != nil && strings.TrimSpace(d.InstanceDiscovery.Query) != "" {
		if len(d.GroupBy) == 0 || kind == "no_data" || kind == "any_of" || kind == "all_of" {
			return d, fmt.Errorf("instance_discovery requires a non-composite grouped numeric or log_match alert")
		}
		if err := storage.ValidateReadOnlySQL(d.InstanceDiscovery.Query); err != nil {
			return d, fmt.Errorf("instance_discovery.query: %w", err)
		}
		if _, err := parseDuration(d.InstanceDiscovery.Every); err != nil {
			return d, fmt.Errorf("instance_discovery.every: %w", err)
		}
		if _, err := parseDuration(d.InstanceDiscovery.StaleAfter); err != nil {
			return d, fmt.Errorf("instance_discovery.stale_after: %w", err)
		}
	}
	return d, nil
}
func (d Definition) Alert(id string) (*model.AlertRule, error) {
	if d.ID != "" {
		id = d.ID
	}
	p, err := parseDuration(d.PendingFor)
	if err != nil {
		return nil, fmt.Errorf("pending_for: %w", err)
	}
	c, err := parseDuration(d.Cooldown)
	if err != nil {
		return nil, fmt.Errorf("cooldown: %w", err)
	}
	repeatInterval, err := parseDuration(d.RepeatInterval)
	if err != nil {
		return nil, fmt.Errorf("repeat_interval: %w", err)
	}
	discoverySQL, discoveryEvery, discoveryStale := "", int64(0), int64(0)
	if d.InstanceDiscovery != nil && strings.TrimSpace(d.InstanceDiscovery.Query) != "" {
		discoverySQL = strings.TrimSpace(d.InstanceDiscovery.Query)
		if discoveryEvery, err = parseDuration(d.InstanceDiscovery.Every); err != nil {
			return nil, fmt.Errorf("instance_discovery.every: %w", err)
		}
		if discoveryStale, err = parseDuration(d.InstanceDiscovery.StaleAfter); err != nil {
			return nil, fmt.Errorf("instance_discovery.stale_after: %w", err)
		}
	}
	kind := d.Condition.Kind
	if kind == "" {
		kind = "threshold"
	}
	conditionMap := map[string]any{"kind": kind}
	if kind == "log_match" {
		conditionMap["pattern"] = d.Condition.Pattern
		if d.Condition.Operator != "" {
			threshold := d.Condition.Threshold
			if threshold == nil {
				threshold = d.Condition.Value
			}
			conditionMap["operator"] = d.Condition.Operator
			conditionMap["value"] = *threshold
		}
	} else if kind == "any_of" || kind == "all_of" {
		conditionMap["rule_ids"] = d.Condition.RuleIDs
	} else if kind != "no_data" {
		threshold := d.Condition.Threshold
		if threshold == nil {
			threshold = d.Condition.Value
		}
		conditionMap["operator"] = d.Condition.Operator
		conditionMap["value"] = *threshold
	}
	condition, _ := json.Marshal(conditionMap)
	groups, _ := json.Marshal(d.GroupBy)
	annotations, _ := json.Marshal(d.Annotations)
	enabled, browser, push := true, true, true
	if d.Enabled != nil {
		enabled = *d.Enabled
	}
	if d.BrowserEnabled != nil {
		browser = *d.BrowserEnabled
	}
	if d.PushoverEnabled != nil {
		push = *d.PushoverEnabled
	}
	return &model.AlertRule{ID: id, Name: d.Name, QuerySQL: d.Query, QueryVersion: 1, ConditionJSON: string(condition), GroupByJSON: string(groups), PendingForNs: p, CooldownNs: c, RepeatIntervalNs: repeatInterval, Severity: d.Severity, Enabled: enabled, BrowserEnabled: browser, PushoverEnabled: push, InstanceDiscoverySQL: discoverySQL, InstanceDiscoveryIntervalNs: discoveryEvery, InstanceDiscoveryStaleAfterNs: discoveryStale, AnnotationsJSON: string(annotations)}, nil
}
func Marshal(r *model.AlertRule) ([]byte, error) {
	var c Condition
	var raw struct {
		Kind     string   `json:"kind"`
		Operator string   `json:"operator"`
		Value    float64  `json:"value"`
		Pattern  string   `json:"pattern"`
		RuleIDs  []string `json:"rule_ids"`
	}
	if err := json.Unmarshal([]byte(r.ConditionJSON), &raw); err != nil {
		return nil, err
	}
	if raw.Kind == "no_data" {
		c = Condition{Kind: "no_data"}
	} else if raw.Kind == "log_match" {
		c = Condition{Kind: "log_match", Pattern: raw.Pattern}
		if raw.Operator != "" {
			c.Operator, c.Threshold = raw.Operator, &raw.Value
		}
	} else if raw.Kind == "any_of" || raw.Kind == "all_of" {
		c = Condition{Kind: raw.Kind, RuleIDs: raw.RuleIDs}
	} else {
		c = Condition{Kind: raw.Kind, Operator: raw.Operator, Threshold: &raw.Value}
	}
	var groups []string
	var a map[string]string
	_ = json.Unmarshal([]byte(r.GroupByJSON), &groups)
	_ = json.Unmarshal([]byte(r.AnnotationsJSON), &a)
	e, b, p := r.Enabled, r.BrowserEnabled, r.PushoverEnabled
	var discovery *InstanceDiscovery
	if r.InstanceDiscoverySQL != "" {
		discovery = &InstanceDiscovery{Query: r.InstanceDiscoverySQL, Every: durationString(r.InstanceDiscoveryIntervalNs), StaleAfter: durationString(r.InstanceDiscoveryStaleAfterNs)}
	}
	return yaml.Marshal(Definition{Version: Version, ID: r.ID, Name: r.Name, Query: r.QuerySQL, Condition: c, GroupBy: groups, PendingFor: durationString(r.PendingForNs), Cooldown: durationString(r.CooldownNs), RepeatInterval: durationString(r.RepeatIntervalNs), Severity: r.Severity, Enabled: &e, BrowserEnabled: &b, PushoverEnabled: &p, InstanceDiscovery: discovery, Annotations: a})
}

func durationString(ns int64) string {
	if ns == 0 {
		return "0s"
	}
	return time.Duration(ns).String()
}
func parseDuration(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	d, e := time.ParseDuration(s)
	if e != nil {
		return 0, fmt.Errorf("must be a Go duration such as 5m or 30s")
	}
	if d < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	return d.Nanoseconds(), nil
}
