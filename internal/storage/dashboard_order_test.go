package storage

import "testing"

func TestMoveDashboardPanel(t *testing.T) {
	db := openTestDB(t)
	d, err := db.CreateDashboard("Order", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"A", "B", "C"} {
		if err := db.CreateDashboardPanel(&DashboardPanel{DashboardID: d.ID, Title: title, QuerySQL: "SELECT 1", DisplayType: "single_value", SettingsJSON: "{}"}); err != nil {
			t.Fatal(err)
		}
	}
	d, _ = db.GetDashboard(d.ID)
	a, b, c := d.Panels[0].ID, d.Panels[1].ID, d.Panels[2].ID
	check := func(ids ...string) {
		t.Helper()
		got, err := db.GetDashboard(d.ID)
		if err != nil {
			t.Fatal(err)
		}
		for i, id := range ids {
			if got.Panels[i].ID != id || got.Panels[i].QuerySQL != "SELECT 1" {
				t.Fatalf("unexpected panel at %d: %+v", i, got.Panels[i])
			}
		}
	}
	if err := db.MoveDashboardPanel(d.ID, b, -1); err != nil {
		t.Fatal(err)
	}
	check(b, a, c)
	if err := db.MoveDashboardPanel(d.ID, b, -1); err != nil {
		t.Fatal(err)
	}
	check(b, a, c)
	if err := db.MoveDashboardPanel(d.ID, a, 1); err != nil {
		t.Fatal(err)
	}
	check(b, c, a)
	if err := db.MoveDashboardPanel(d.ID, a, 1); err != nil {
		t.Fatal(err)
	}
	check(b, c, a)
	if err := db.MoveDashboardPanel(d.ID, "missing", 1); err == nil {
		t.Fatal("missing panel accepted")
	}
	if err := db.MoveDashboardPanel(d.ID, a, 0); err == nil {
		t.Fatal("invalid direction accepted")
	}
	check(b, c, a)
}
