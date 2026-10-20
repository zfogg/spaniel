package dashboardconfig

import (
	"fmt"
	"strings"
)

// ValidatePanelResult checks the column contract required by a dashboard
// renderer. HTTP previews/imports and MCP validation share this boundary.
func ValidatePanelResult(display string, columns []string) error {
	if display == "" || display == "table" {
		return nil
	}
	has := func(names ...string) bool {
		for _, candidate := range names {
			for _, column := range columns {
				if strings.EqualFold(column, candidate) {
					return true
				}
			}
		}
		return false
	}
	switch display {
	case "single_value":
		if !has("value") {
			return fmt.Errorf("single value panels require a value column")
		}
	case "time_series", "deploy_correlation":
		if !has("value") || !has("timestamp", "timestamp_ns", "time_ns") {
			return fmt.Errorf("time series panels require timestamp and value columns")
		}
	case "heatmap":
		axisColumns := []string{"timestamp_ns", "timestamp", "time_ns", "time", "x"}
		axisCount := 0
		for _, axis := range axisColumns {
			if has(axis) {
				axisCount++
			}
		}
		if !has("value") || axisCount == 0 {
			return fmt.Errorf("heatmap panels require value and a time/x column (timestamp_ns, timestamp, time_ns, time, or x); include bucket_ms (or y) for a two-dimensional heatmap")
		}
		if axisCount > 1 {
			return fmt.Errorf("heatmap panels require exactly one time/x column; choose one of timestamp_ns, timestamp, time_ns, time, or x")
		}
	case "entity_list":
		if !has("label", "service_name", "name") || !has("primary_value", "value", "duration_ns") {
			return fmt.Errorf("entity list panels require a label and primary_value (or value) column")
		}
	case "trace_list":
		if !has("trace_id") {
			return fmt.Errorf("trace list panels require a trace_id column")
		}
	case "span_list":
		if !has("span_id", "trace_id") {
			return fmt.Errorf("span list panels require a span_id or trace_id column")
		}
	case "log_list":
		if !has("body", "message") {
			return fmt.Errorf("log list panels require a body or message column")
		}
	}
	return nil
}

// ValidateDeployAnnotationResult checks the auxiliary release-marker query.
func ValidateDeployAnnotationResult(columns []string) error {
	has := func(names ...string) bool {
		for _, candidate := range names {
			for _, column := range columns {
				if strings.EqualFold(column, candidate) {
					return true
				}
			}
		}
		return false
	}
	if !has("timestamp", "timestamp_ns", "time_ns") {
		return fmt.Errorf("deploy annotation_query requires a timestamp, timestamp_ns, or time_ns column")
	}
	if !has("label", "release", "version") {
		return fmt.Errorf("deploy annotation_query requires a label, release, or version column")
	}
	return nil
}
