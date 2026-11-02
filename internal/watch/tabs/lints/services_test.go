package lints

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/watch/client"
)

func TestServiceLookupPagesWithinActualServerLimit(t *testing.T) {
	// Mirror storage.ListSpans' actual bounds: invalid sizes silently become 100.
	// The correlated span is outside both that default and the first valid page.
	spans := make([]apigen.SpanRow, 1500)
	for i := range spans {
		spans[i] = apigen.SpanRow{SessionId: "demo", TraceId: "trace", SpanId: fmt.Sprintf("span-%04d", i), ServiceName: "other"}
	}
	spans[1100].ServiceName = "beyond-first-page"
	pages := []int{}
	var pagesMu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sessionId") != "demo" {
			t.Error("unscoped lookup")
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 1000 {
			limit = 100
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		pagesMu.Lock()
		pages = append(pages, page)
		pagesMu.Unlock()
		start := min(len(spans), (page-1)*limit)
		end := min(len(spans), start+limit)
		total := len(spans)
		json.NewEncoder(w).Encode(client.Envelope[[]apigen.SpanRow]{Data: spans[start:end], Meta: &apigen.Meta{Total: &total}})
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	api, err := client.New(srv.URL, client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	env := testEnv("lints")
	env.API = api
	env.Refresh = client.NewRefreshes(context.Background(), api)
	env.Refresh.SetSession("demo")
	m := New(env).(*model)
	deliver(m, "lint", client.Envelope[[]apigen.LintWarning]{Data: []apigen.LintWarning{{SessionId: "demo", TraceId: "trace", SpanId: "span-1100", RuleId: "http.method", Severity: "warning"}}})
	deliver(m, "issues", client.Envelope[[]apigen.TraceIssue]{})
	// Exercise the registered production Fetch rather than injecting services.
	for _, work := range env.Refresh.Due(time.Now()) {
		if work.Key.Name != "services" {
			continue
		}
		result := work.Run()
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		m.Update(result)
	}
	pagesMu.Lock()
	defer pagesMu.Unlock()
	if len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Fatalf("supported paging not used: %v", pages)
	}
	m.filter = "service:beyond-first-page"
	m.refilter()
	if len(m.visible) != 1 || m.visible[0].span != "span-1100" {
		t.Fatalf("service filter missed correlated finding after first 100 spans: %+v", m.visible)
	}
	if len(m.services) != len(spans) {
		t.Fatalf("lookup only covered %d spans", len(m.services))
	}
}
