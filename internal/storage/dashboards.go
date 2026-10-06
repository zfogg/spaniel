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
	return x, d.query.Dashboard.Create(x)
}
func (d *DB) UpdateDashboard(x *Dashboard) error {
	x.UpdatedAt = time.Now().UnixNano()
	return d.query.Dashboard.Save(x)
}
func (d *DB) DeleteDashboard(id string) error {
	return d.query.Transaction(func(tx *querygen.Query) error {
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
func (d *DB) SaveDashboardVariable(v *DashboardVariable) error {
	return d.query.DashboardVariable.Save(v)
}
func (d *DB) DeleteDashboardVariable(id, name string) error {
	_, err := d.query.DashboardVariable.Where(
		d.query.DashboardVariable.DashboardID.Eq(id),
		d.query.DashboardVariable.Name.Eq(name),
	).Delete()
	return err
}
func (d *DB) CreateDashboardPanel(p *DashboardPanel) error {
	p.ID = uuid.NewString()
	p.UpdatedAt = time.Now().UnixNano()
	if p.Position < 0 {
		p.Position = 0
	}
	return d.query.DashboardPanel.Create(p)
}
func (d *DB) UpdateDashboardPanel(p *DashboardPanel) error {
	p.UpdatedAt = time.Now().UnixNano()
	// DuckDB's ART index does not allow an indexed key to be deleted and
	// reinserted in one transaction. Commit the delete before recreating the
	// stable ID; the API still returns the replacement only on success.
	if _, err := d.query.DashboardPanel.Where(
		d.query.DashboardPanel.ID.Eq(p.ID),
		d.query.DashboardPanel.DashboardID.Eq(p.DashboardID),
	).Delete(); err != nil {
		return err
	}
	return d.query.DashboardPanel.Create(p)
}
func (d *DB) DeleteDashboardPanel(dashboardID, id string) error {
	_, err := d.query.DashboardPanel.Where(
		d.query.DashboardPanel.ID.Eq(id),
		d.query.DashboardPanel.DashboardID.Eq(dashboardID),
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
