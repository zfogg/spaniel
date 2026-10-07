package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage/querygen"
	"sort"
	"time"
)

type Dashboard = model.Dashboard
type DashboardVariable = model.DashboardVariable
type DashboardPanel = model.DashboardPanel

// MoveDashboardPanel changes only ordering, atomically, including older tied positions.
func (d *DB) MoveDashboardPanel(dashboardID, panelID string, direction int) error {
	if direction != -1 && direction != 1 {
		return fmt.Errorf("direction must be -1 or 1")
	}
	return d.namedQuery("storage.MoveDashboardPanel").Transaction(func(tx *querygen.Query) error {
		p := tx.DashboardPanel
		panels, err := p.Where(p.DashboardID.Eq(dashboardID)).Order(p.Position, p.Title, p.ID).Find()
		if err != nil {
			return err
		}
		index := -1
		for i, panel := range panels {
			if panel.ID == panelID {
				index = i
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("panel not found")
		}
		next := index + direction
		if next < 0 || next >= len(panels) {
			return nil
		}
		panels[index], panels[next] = panels[next], panels[index]
		for i, panel := range panels {
			if _, err := p.Where(p.DashboardID.Eq(dashboardID), p.ID.Eq(panel.ID)).UpdateColumn(p.Position, i); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) ListDashboards() ([]*Dashboard, error) {
	out, err := d.query.Dashboard.Order(d.query.Dashboard.UpdatedAt.Desc()).Find()
	if err != nil {
		return nil, err
	}
	order, err := d.query.Meta.Where(d.query.Meta.Key.Eq("dashboard_order")).Find()
	if err != nil {
		return nil, err
	}
	if len(order) > 0 {
		var ids []string
		if err := json.Unmarshal([]byte(order[0].Value), &ids); err != nil {
			return nil, err
		}
		ranks := make(map[string]int, len(ids))
		for i, id := range ids {
			ranks[id] = i
		}
		rank := func(id string) int {
			if i, ok := ranks[id]; ok {
				return i
			}
			return len(ids)
		}
		sort.SliceStable(out, func(i, j int) bool { return rank(out[i].ID) < rank(out[j].ID) })
	}
	for _, x := range out {
		if err := d.hydrateDashboard(x); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Persist the list as one metadata value so ordering changes are atomic and
// cannot overwrite dashboard definitions. New dashboards remain visible.
func (d *DB) ReorderDashboards(ids []string) error {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return d.namedQuery("storage.ReorderDashboards").Meta.UpsertValue("dashboard_order", string(encoded))
}
func (d *DB) GetDashboard(id string) (*Dashboard, error) {
	x, err := d.query.Dashboard.Where(d.query.Dashboard.ID.Eq(id)).First()
	if err != nil {
		return nil, err
	}
	return x, d.hydrateDashboard(x)
}
func (d *DB) hydrateDashboard(x *Dashboard) error {
	variables, err := d.query.DashboardVariable.Where(d.query.DashboardVariable.DashboardID.Eq(x.ID)).Order(d.query.DashboardVariable.Name).Find()
	if err != nil {
		return err
	}
	x.Variables = variables
	panels, err := d.query.DashboardPanel.Where(d.query.DashboardPanel.DashboardID.Eq(x.ID)).Order(d.query.DashboardPanel.Position, d.query.DashboardPanel.Title, d.query.DashboardPanel.ID).Find()
	x.Panels = panels
	return err
}
func (d *DB) CreateDashboard(name, description string) (*Dashboard, error) {
	return d.CreateDashboardWithPanels(name, description, nil)
}

// CreateDashboardWithPanels keeps template creation atomic: either the complete
// dashboard is persisted, or no dashboard or child panels are left behind.
func (d *DB) CreateDashboardWithPanels(name, description string, panels []*DashboardPanel) (*Dashboard, error) {
	now := time.Now().UnixNano()
	x := &Dashboard{ID: uuid.NewString(), Name: name, Description: description, CreatedAt: now, UpdatedAt: now, Variables: []*DashboardVariable{}, Panels: []*DashboardPanel{}}
	err := d.namedQuery("storage.CreateDashboard").Transaction(func(tx *querygen.Query) error {
		if err := tx.Dashboard.Create(x); err != nil {
			return err
		}
		for _, variable := range []*DashboardVariable{
			{DashboardID: x.ID, Name: "service", Kind: "string", Source: "spans.service_name", OptionsJSON: "[]"},
			{DashboardID: x.ID, Name: "operation", Kind: "string", Source: "spans.name", OptionsJSON: "[]"},
			{DashboardID: x.ID, Name: "status_code", Kind: "number", Source: "spans.status_code", DefaultValue: "0", OptionsJSON: "[]"},
			{DashboardID: x.ID, Name: "severity", Kind: "number", Source: "logs.severity", DefaultValue: "9", OptionsJSON: "[]"},
		} {
			if err := tx.DashboardVariable.Create(variable); err != nil {
				return err
			}
			x.Variables = append(x.Variables, variable)
		}
		for position, panel := range panels {
			p := *panel
			p.ID, p.DashboardID, p.UpdatedAt, p.Position = uuid.NewString(), x.ID, now, position
			if err := tx.DashboardPanel.Create(&p); err != nil {
				return err
			}
			x.Panels = append(x.Panels, &p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return x, nil
}
func (d *DB) UpdateDashboard(x *Dashboard) error {
	x.UpdatedAt = time.Now().UnixNano()
	return d.namedQuery("storage.UpdateDashboard").Dashboard.Save(x)
}
func (d *DB) DeleteDashboard(id string) error {
	return d.namedQuery("storage.DeleteDashboard").Transaction(func(tx *querygen.Query) error {
		if _, err := tx.DashboardPanel.Where(tx.DashboardPanel.DashboardID.Eq(id)).Delete(); err != nil {
			return err
		}
		if _, err := tx.DashboardVariable.Where(tx.DashboardVariable.DashboardID.Eq(id)).Delete(); err != nil {
			return err
		}
		_, err := tx.Dashboard.Where(tx.Dashboard.ID.Eq(id)).Delete()
		return err
	})
}

// ReplaceDashboardDefinition replaces a dashboard and all declarative children.
// DuckDB cannot delete and reinsert the dashboard_id index key in one
// transaction, so the deletion must commit before the replacement is written.
// It is used for file/API imports, never for telemetry.
func (d *DB) ReplaceDashboardDefinition(x *Dashboard) error {
	now := time.Now().UnixNano()
	x.CreatedAt, x.UpdatedAt = now, now
	for _, panel := range x.Panels {
		panel.ID = uuid.NewString()
		panel.DashboardID = x.ID
		panel.UpdatedAt = now
	}
	for _, variable := range x.Variables {
		variable.DashboardID = x.ID
	}
	if err := d.DeleteDashboard(x.ID); err != nil {
		return err
	}
	q := d.namedQuery("storage.ReplaceDashboardDefinition")
	// Create the parent on its own. GORM otherwise persists the preloaded
	// association slices here and the explicit child creates below attempt to
	// write them a second time.
	dashboard := *x
	dashboard.Variables = nil
	dashboard.Panels = nil
	if err := q.Dashboard.Create(&dashboard); err != nil {
		return err
	}
	if len(x.Variables) > 0 {
		if err := q.DashboardVariable.Create(x.Variables...); err != nil {
			return err
		}
	}
	if len(x.Panels) > 0 {
		if err := q.DashboardPanel.Create(x.Panels...); err != nil {
			return err
		}
	}
	return nil
}
func (d *DB) SaveDashboardVariable(v *DashboardVariable) error {
	return d.namedQuery("storage.SaveDashboardVariable").DashboardVariable.Save(v)
}
func (d *DB) DeleteDashboardVariable(id, name string) error {
	q := d.namedQuery("storage.DeleteDashboardVariable")
	_, err := q.DashboardVariable.Where(
		q.DashboardVariable.DashboardID.Eq(id),
		q.DashboardVariable.Name.Eq(name),
	).Delete()
	return err
}
func (d *DB) CreateDashboardPanel(p *DashboardPanel) error {
	p.ID = uuid.NewString()
	p.UpdatedAt = time.Now().UnixNano()
	if p.Position < 0 {
		p.Position = 0
	}
	return d.namedQuery("storage.CreateDashboardPanel").DashboardPanel.Create(p)
}
func (d *DB) UpdateDashboardPanel(p *DashboardPanel) error {
	p.UpdatedAt = time.Now().UnixNano()
	// DuckDB's ART index does not allow an indexed key to be deleted and
	// reinserted in one transaction. Commit the delete before recreating the
	// stable ID; the API still returns the replacement only on success.
	q := d.namedQuery("storage.UpdateDashboardPanel")
	if _, err := q.DashboardPanel.Where(
		q.DashboardPanel.ID.Eq(p.ID),
		q.DashboardPanel.DashboardID.Eq(p.DashboardID),
	).Delete(); err != nil {
		return err
	}
	return q.DashboardPanel.Create(p)
}
func (d *DB) DeleteDashboardPanel(dashboardID, id string) error {
	q := d.namedQuery("storage.DeleteDashboardPanel")
	_, err := q.DashboardPanel.Where(
		q.DashboardPanel.ID.Eq(id),
		q.DashboardPanel.DashboardID.Eq(dashboardID),
	).Delete()
	return err
}
func (d *DB) DashboardRows(query string, args ...any) ([]map[string]any, error) {
	cols, rows, _, err := d.ReadOnlyQueryArgs(context.Background(), query, args, 1000)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, vs := range rows {
		m := map[string]any{}
		for i, c := range cols {
			switch v := vs[i].(type) {
			case []byte:
				m[c] = string(v)
			case nil:
				m[c] = nil
			default:
				m[c] = v
			}
		}
		out = append(out, m)
	}
	return out, nil
}

var _ = sql.ErrNoRows
