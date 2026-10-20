package api

import (
	"fmt"
	"github.com/zfogg/spaniel/internal/storage"
	"net/http"
	"testing"
)

func TestNotificationsAPI_PaginatesBrowserHistory(t *testing.T) {
	handler, store := setupRouter(t)
	for i := 0; i < 36; i++ {
		if err := store.RecordNotification(&storage.NotificationRecord{ID: fmt.Sprintf("notice-%02d", i), Source: "alert", Title: "Browser toast", Severity: "warning", CreatedAt: int64(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		page, count int
		first       string
	}{{1, 35, "notice-35"}, {2, 1, "notice-00"}} {
		res := do(t, handler, http.MethodGet, fmt.Sprintf("/api/notifications?page=%d&limit=35", tc.page), nil)
		if res.Code != http.StatusOK {
			t.Fatalf("status %d: %s", res.Code, res.Body.String())
		}
		rows := decodeData[[]storage.NotificationRecord](t, res.Body.Bytes())
		if len(rows) != tc.count || rows[0].ID != tc.first {
			t.Fatalf("page %d: %#v", tc.page, rows)
		}
	}
	created := do(t, handler, http.MethodPost, "/api/alerts", alertPayload())
	rule := decodeData[map[string]any](t, created.Body.Bytes())
	id := rule["id"].(string)
	res := do(t, handler, http.MethodPost, "/api/alerts/"+id+"/test-notification", map[string]string{"destination": "browser"})
	if res.Code != http.StatusOK {
		t.Fatalf("test toast: %d %s", res.Code, res.Body.String())
	}
	rows, total, err := store.ListNotifications("alert", 1, 35)
	if err != nil || total != 37 || rows[0].SourceID != id {
		t.Fatalf("browser test wasn't persisted: %d %v", total, err)
	}
}
