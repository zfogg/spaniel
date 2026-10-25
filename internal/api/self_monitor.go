package api

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	bundled "github.com/zfogg/spaniel/deploy/spaniel"
	"github.com/zfogg/spaniel/internal/alertconfig"
	"github.com/zfogg/spaniel/internal/dashboardconfig"
)

type selfMonitorInstallResult struct {
	Created   int `json:"created"`
	Refreshed int `json:"refreshed"`
	Skipped   int `json:"skipped"`
}

func (r *Router) installSelfMonitorDashboards(w http.ResponseWriter, req *http.Request) {
	r.installSelfMonitor(w, req, "dashboards")
}
func (r *Router) installSelfMonitorAlerts(w http.ResponseWriter, req *http.Request) {
	r.installSelfMonitor(w, req, "alerts")
}

func (r *Router) installSelfMonitor(w http.ResponseWriter, req *http.Request, kind string) {
	r.selfMonitorInstallMu.Lock()
	defer r.selfMonitorInstallMu.Unlock()
	result, err := r.installSelfMonitorDefinitions(req, kind)
	if err != nil {
		respondErr(w, req, 500, "install self-monitoring definitions: "+err.Error())
		return
	}
	respond(w, result, 1, 1)
}

func (r *Router) installSelfMonitorDefinitions(req *http.Request, kind string) (selfMonitorInstallResult, error) {
	var result selfMonitorInstallResult
	store := r.store.WithContext(req.Context())
	names := map[string]string{}
	if kind == "dashboards" {
		existing, err := store.ListDashboards()
		if err != nil {
			return result, err
		}
		for _, x := range existing {
			names[x.Name] = x.ID
		}
	} else {
		existing, err := store.ListAlertRules()
		if err != nil {
			return result, err
		}
		for _, x := range existing {
			names[x.Name] = x.ID
		}
	}
	files, err := bundled.Definitions.ReadDir(kind)
	if err != nil {
		return result, err
	}
	for _, file := range files {
		data, err := bundled.Definitions.ReadFile(kind + "/" + file.Name())
		if err != nil {
			return result, err
		}
		if kind == "dashboards" {
			definition, err := dashboardconfig.Parse(data)
			if err != nil {
				return result, fmt.Errorf("%s: %w", file.Name(), err)
			}
			id, exists := names[definition.Name]
			if !exists {
				id = uuid.NewString()
			}
			// Installation matches names, never the deployment YAML's fixed ID.
			definition.ID = id
			dashboard, err := definition.Dashboard(id)
			if err != nil {
				return result, err
			}
			if err := store.ReplaceDashboardDefinition(dashboard); err != nil {
				return result, err
			}
			names[definition.Name] = id
			if exists {
				result.Refreshed++
			} else {
				result.Created++
			}
		} else {
			definition, err := alertconfig.Parse(data)
			if err != nil {
				return result, fmt.Errorf("%s: %w", file.Name(), err)
			}
			id, exists := names[definition.Name]
			if !exists {
				id = uuid.NewString()
			}
			definition.ID = id
			rule, err := definition.Alert(definition.ID)
			if err != nil {
				return result, err
			}
			if err := store.ReplaceAlertDefinition(rule); err != nil {
				return result, err
			}
			names[definition.Name] = rule.ID
			if exists {
				result.Refreshed++
			} else {
				result.Created++
			}
		}
	}
	return result, nil
}
