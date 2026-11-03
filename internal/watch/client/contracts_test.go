package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/zfogg/spaniel/internal/api/apigen"
)

func TestCanonicalLinksCardinalityAndExactDynamicNumbers(t *testing.T) {
	srv := server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/dashboards/d/query-preview" && r.URL.Query().Get("sessionId") != "pinned" {
			t.Error("missing selected scope")
		}
		switch r.URL.Path {
		case "/api/traces/t/incoming-links":
			w.Write([]byte(`{"data":[{"trace_id":"source","span_id":"parent","session_id":"pinned","linked_trace_id":"t","linked_span_id":"child","attributes":"{\"count\":9007199254740993}"}]}`))
		case "/api/metrics/cardinality":
			w.Write([]byte(`{"data":[{"service_name":"api","name":"requests","active_series":37,"limit":2000}]}`))
		case "/api/dashboards/d/query-preview":
			var body apigen.PreviewDashboardQueryJSONBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SessionId == nil || *body.SessionId != "pinned" {
				t.Error("missing preview scope", err)
			}
			w.Write([]byte(`{"data":{"columns":["exact","nested"],"rows":[{"exact":9007199254740993,"nested":{"array":[18446744073709551615]}}]}}`))
		default:
			t.Error("unexpected request", r.URL.Path)
		}
	}))
	c, _ := New(srv.URL, Options{})
	ctx := context.Background()
	q := url.Values{"sessionId": {"pinned"}}
	links, err := c.IncomingLinks(ctx, "t", q)
	if err != nil || len(links.Data) != 1 || links.Data[0].LinkedTraceId != "t" || links.Data[0].LinkedSpanId != "child" || links.Data[0].Attributes != `{"count":9007199254740993}` {
		t.Fatal(links, err)
	}
	budget, err := c.MetricCardinality(ctx, q)
	if err != nil || len(budget.Data) != 1 || budget.Data[0].ActiveSeries != 37 {
		t.Fatal(budget, err)
	}
	session := "pinned"
	preview, err := c.Preview(ctx, "d", apigen.PreviewDashboardQueryJSONBody{QuerySql: "SELECT 1", SessionId: &session})
	if err != nil {
		t.Fatal(err)
	}
	row := preview.Data.Rows[0]
	if row["exact"] != json.Number("9007199254740993") || row["nested"].(map[string]any)["array"].([]any)[0] != json.Number("18446744073709551615") {
		t.Fatal("dynamic integer rounded", row)
	}
}
