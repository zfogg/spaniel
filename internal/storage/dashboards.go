package storage

import (
	"database/sql"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

type Dashboard struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	CreatedAt   int64                `json:"created_at"`
	UpdatedAt   int64                `json:"updated_at"`
	Variables   []*DashboardVariable `json:"variables"`
	Panels      []*DashboardPanel    `json:"panels"`
}

func (Dashboard) TableName() string { return "dashboards" }

type DashboardVariable struct {
	DashboardID  string `json:"dashboard_id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Source       string `json:"source"`
	OptionsJSON  string `json:"options_json"`
	DefaultValue string `json:"default_value"`
}

func (DashboardVariable) TableName() string { return "dashboard_variables" }

type DashboardPanel struct {
	ID           string `json:"id"`
	DashboardID  string `json:"dashboard_id"`
	Title        string `json:"title"`
	DisplayType  string `json:"display_type"`
	QuerySQL     string `json:"query_sql"`
	QueryVersion int    `json:"query_version"`
	SettingsJSON string `json:"settings_json"`
	LayoutJSON   string `json:"layout_json"`
	Position     int    `json:"position"`
	UpdatedAt    int64  `json:"updated_at"`
}

func (DashboardPanel) TableName() string { return "dashboard_panels" }

func (d *DB) ListDashboards() ([]*Dashboard, error) {
	out := []*Dashboard{}
	if err := d.gorm.Order("updated_at DESC").Find(&out).Error; err != nil {
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
	var x Dashboard
	if err := d.gorm.First(&x, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &x, d.hydrateDashboard(&x)
}
func (d *DB) hydrateDashboard(x *Dashboard) error {
	if err := d.gorm.Where("dashboard_id = ?", x.ID).Order("name").Find(&x.Variables).Error; err != nil {
		return err
	}
	return d.gorm.Where("dashboard_id = ?", x.ID).Order("position, title").Find(&x.Panels).Error
}
func (d *DB) CreateDashboard(name, description string) (*Dashboard, error) {
	now := time.Now().UnixNano()
	x := &Dashboard{ID: uuid.NewString(), Name: name, Description: description, CreatedAt: now, UpdatedAt: now, Variables: []*DashboardVariable{}, Panels: []*DashboardPanel{}}
	return x, d.gorm.Create(x).Error
}
func (d *DB) UpdateDashboard(x *Dashboard) error {
	x.UpdatedAt = time.Now().UnixNano()
	return d.gorm.Model(&Dashboard{}).Where("id = ?", x.ID).Updates(map[string]any{"name": x.Name, "description": x.Description, "updated_at": x.UpdatedAt}).Error
}
func (d *DB) DeleteDashboard(id string) error {
	return d.gorm.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("dashboard_id = ?", id).Delete(&DashboardPanel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("dashboard_id = ?", id).Delete(&DashboardVariable{}).Error; err != nil {
			return err
		}
		return tx.Delete(&Dashboard{}, "id = ?", id).Error
	})
}
func (d *DB) SaveDashboardVariable(v *DashboardVariable) error { return d.gorm.Save(v).Error }
func (d *DB) DeleteDashboardVariable(id, name string) error {
	return d.gorm.Delete(&DashboardVariable{}, "dashboard_id = ? AND name = ?", id, name).Error
}
func (d *DB) CreateDashboardPanel(p *DashboardPanel) error {
	p.ID = uuid.NewString()
	p.UpdatedAt = time.Now().UnixNano()
	if p.Position < 0 {
		p.Position = 0
	}
	return d.gorm.Create(p).Error
}
func (d *DB) UpdateDashboardPanel(p *DashboardPanel) error {
	p.UpdatedAt = time.Now().UnixNano()
	// DuckDB implements primary-key updates as delete/insert operations, but
	// rejects the replacement while the old ART-index entry is in the same
	// transaction. Commit the delete before recreating the stable panel ID.
	if err := d.gorm.Where("id = ? AND dashboard_id = ?", p.ID, p.DashboardID).Delete(&DashboardPanel{}).Error; err != nil {
		return err
	}
	return d.gorm.Create(p).Error
}
func (d *DB) DeleteDashboardPanel(dashboardID, id string) error {
	return d.gorm.Delete(&DashboardPanel{}, "id = ? AND dashboard_id = ?", id, dashboardID).Error
}
func (d *DB) DashboardRows(query string, args ...any) ([]map[string]any, error) {
	rows, err := d.gorm.Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		vs := make([]any, len(cols))
		ps := make([]any, len(cols))
		for i := range vs {
			ps[i] = &vs[i]
		}
		if err := rows.Scan(ps...); err != nil {
			return nil, err
		}
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
	return out, rows.Err()
}

var _ = sql.ErrNoRows
