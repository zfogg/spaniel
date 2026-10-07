package storage

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"github.com/zfogg/spaniel/internal/model"
	"github.com/zfogg/spaniel/internal/storage/querygen"
	"time"
)

type Dashboard = model.Dashboard
type DashboardVariable = model.DashboardVariable
type DashboardPanel = model.DashboardPanel

func (d *DB) ListDashboards() ([]*Dashboard, error) {
	out, err := d.query.Dashboard.Order(d.query.Dashboard.UpdatedAt.Desc()).Find()
	if err != nil {
		return nil, err
	}
	for _, x := range out {
		if err := d.hydrateDashboard(x); err != nil {
			return nil, err
		}
	}
	return out, nil
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
	panels, err := d.query.DashboardPanel.Where(d.query.DashboardPanel.DashboardID.Eq(x.ID)).Order(d.query.DashboardPanel.Position, d.query.DashboardPanel.Title).Find()
	x.Panels = panels
	return err
}
func (d *DB) CreateDashboard(name, description string) (*Dashboard, error) {
	now := time.Now().UnixNano()
	x := &Dashboard{ID: uuid.NewString(), Name: name, Description: description, CreatedAt: now, UpdatedAt: now, Variables: []*DashboardVariable{}, Panels: []*DashboardPanel{}}
	return x, d.namedQuery("storage.CreateDashboard").Dashboard.Create(x)
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
