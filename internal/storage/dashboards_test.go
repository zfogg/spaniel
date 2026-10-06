package storage

import "testing"

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
