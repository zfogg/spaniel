package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestSignalServicesAndMultiServicePagination(t *testing.T) {
	handler, store := setupRouter(t)
	store.SetActiveSession("session-a", "A")
	insertSpan(t, store, "session-a", "rare-trace", "rare-span", "", "rare", "work")
	insertSpan(t, store, "session-a", "other-trace", "other-span", "", "other", "work")
	for i := 0; i < 105; i++ {
		insertSpan(t, store, "session-a", fmt.Sprintf("trace-%d", i), fmt.Sprintf("span-%d", i), "", "busy", "work")
	}
	insertSpan(t, store, "session-a", "child-trace", "child-span", "remote-parent", "child-only", "child")
	insertSpan(t, store, "session-b", "foreign-trace", "foreign-span", "", "foreign", "work")
	for _, service := range []string{"busy", "rare", "other", "logs-metrics-only"} {
		if err := store.InsertLog(&storage.Log{SessionID: "session-a", ServiceName: service, Body: service, Attributes: "{}", TimestampNs: 1}); err != nil {
			t.Fatal(err)
		}
		if err := store.InsertMetric(&storage.Metric{SessionID: "session-a", ServiceName: service, Name: "requests", Type: "gauge", Attributes: "{}", TimestampNs: 1}); err != nil {
			t.Fatal(err)
		}
	}
	for signal, want := range map[string][]string{
		"traces":  {"busy", "other", "rare"},
		"spans":   {"busy", "child-only", "other", "rare"},
		"logs":    {"busy", "logs-metrics-only", "other", "rare"},
		"metrics": {"busy", "logs-metrics-only", "other", "rare"},
	} {
		got := getStringData(t, handler, "/api/services?signal="+signal)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s services = %v, want %v", signal, got, want)
		}
	}
	if got := getStringData(t, handler, "/api/services?signal=spans&sessionId=session-b"); !reflect.DeepEqual(got, []string{"foreign"}) {
		t.Fatalf("session override: %v", got)
	}
	// The rare services must remain discoverable and selectable even when a busy
	// service fills the first hundred rows. Filters apply before LIMIT and COUNT.
	for _, endpoint := range []string{"traces", "spans", "spans?view=grouped", "logs"} {
		separator := "?"
		if endpoint == "spans?view=grouped" {
			separator = "&"
		}
		for page := 1; page <= 2; page++ {
			path := fmt.Sprintf("/api/%s%sservices=rare&services=other&limit=1&page=%d", endpoint, separator, page)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != 200 {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
			}
			var response struct {
				Data []struct {
					ServiceName string `json:"service_name"`
				}
				Meta struct{ Total int }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Meta.Total != 2 || len(response.Data) != 1 {
				t.Fatalf("%s: %s", path, w.Body.String())
			}
			if service := response.Data[0].ServiceName; service != "rare" && service != "other" {
				t.Fatalf("unexpected service %s", service)
			}
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/services?signal=invalid", nil))
	if w.Code != 400 {
		t.Fatalf("invalid signal status = %d", w.Code)
	}
}
