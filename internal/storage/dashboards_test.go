package storage

import "testing"

func TestDashboardTemplateRollback(t *testing.T) {
	db := openTestDB(t)
	// Simulate a schema/storage failure during panel creation in this isolated DB.
	if err := db.gorm.Exec("DROP TABLE dashboard_panels").Error; err != nil {
		t.Fatal(err)
	}
	_, err := db.CreateDashboardWithPanels("Must not persist", "", []*DashboardPanel{{Title: "Count", DisplayType: "single_value", QuerySQL: "SELECT 1 AS value"}})
	if err == nil {
		t.Fatal("expected panel storage failure")
	}
	rows, err := db.query.Dashboard.Find()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatal("template failure left a dashboard behind")
	}
}

func TestDashboardPanelPersistenceAndDeletion(t *testing.T) {
	db := openTestDB(t)
	dashboard, err := db.CreateDashboard("Verification", "")
	if err != nil {
		t.Fatal(err)
	}
	panel := &DashboardPanel{DashboardID: dashboard.ID, Title: "Span count", DisplayType: "single_value", QuerySQL: "SELECT count(*) AS value FROM telemetry_spans", QueryVersion: 1, SettingsJSON: "{}", LayoutJSON: "{}"}
	if err := db.CreateDashboardPanel(panel); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.GetDashboard(dashboard.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Panels) != 1 || loaded.Panels[0].ID != panel.ID {
		t.Fatalf("panel not persisted: %+v", loaded.Panels)
	}
	panel.Title = "Slowest spans"
	panel.QuerySQL = "SELECT trace_id FROM telemetry_spans ORDER BY duration_ns DESC"
	if err := db.UpdateDashboardPanel(panel); err != nil {
		t.Fatalf("update panel: %v", err)
	}
	loaded, err = db.GetDashboard(dashboard.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Panels[0]; got.ID != panel.ID || got.Title != panel.Title || got.QuerySQL != panel.QuerySQL {
		t.Fatalf("panel update not persisted: %+v", got)
	}
	if err := db.DeleteDashboard(dashboard.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetDashboard(dashboard.ID); err == nil {
		t.Fatal("deleted dashboard still exists")
	}
	var count int64
	if err := db.gorm.Model(&DashboardPanel{}).Where("dashboard_id = ?", dashboard.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d orphan panels remain", count)
	}
}
