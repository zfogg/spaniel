package storage

import "testing"

func TestDashboardListOrder(t *testing.T) {
	db := openTestDB(t)
	a, err := db.CreateDashboard("A", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateDashboard("B", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{a.ID, b.ID}, {b.ID, a.ID}} {
		if err := db.ReorderDashboards(ids); err != nil {
			t.Fatal(err)
		}
		got, err := db.ListDashboards()
		if err != nil {
			t.Fatal(err)
		}
		if got[0].ID != ids[0] || got[1].ID != ids[1] {
			t.Fatal("order not saved")
		}
	}
	if _, err := db.CreateDashboard("New", ""); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListDashboards()
	if err != nil || len(got) != 3 || got[2].Name != "New" {
		t.Fatalf("new dashboard lost: %+v %v", got, err)
	}
}
