package metrics

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

func TestHTTPNormalizationKeepsExactLabelsAndScopeAttributes(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"name":"requests","type":"sum","points":[],"series":[{"key":"a","attributes":{"exact":9007199254740993,"nested":[18446744073709551615]},"points":[{"timestamp_ns":1793631000000000001,"value":42,"flags":0,"scope_attributes":{"build":9007199254740997}}]}]}}`))
	}))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	api, _ := client.New(srv.URL, client.Options{})
	response, err := api.MetricSeries(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := normalize(response.Data)
	if err != nil {
		t.Fatal(err)
	}
	s := data.Series[0]
	if s.Attributes["exact"] != json.Number("9007199254740993") || s.Attributes["nested"].([]any)[0] != json.Number("18446744073709551615") || s.Points[0].ScopeAttributes["build"] != json.Number("9007199254740997") || s.Points[0].Timestamp != 1793631000000000001 {
		t.Fatal("normalization rounded dynamic values", s)
	}
	view := strings.Join(attributeTable("Labels", s.Attributes, 80), "\n")
	if !strings.Contains(view, "9007199254740993") || !strings.Contains(view, "18446744073709551615") || strings.Contains(view, "e+15") {
		t.Fatal(view)
	}
}

func TestAdmissionSharedRefreshSessionAndHiddenRepair(t *testing.T) {
	env := testEnv(t)
	m := New(env).(*catalog)
	m.Update(catalogResult(m, apigen.MetricCatalogEntry{Name: "requests", ServiceName: "api"}))
	result := protocol.Result{Key: client.Key{Owner: env.Owner, Name: "cardinality"}, Session: "seed", Data: client.Envelope[[]apigen.MetricCardinalityStream]{Data: []apigen.MetricCardinalityStream{{Name: "requests", ServiceName: "api", ActiveSeries: 37, Limit: 2000}}}}
	m.Update(result)
	if !strings.Contains(m.View(), "37 active series / 2000 limit") {
		t.Fatal(m.View())
	}
	d := newDetail(env, apigen.MetricCatalogEntry{Name: "requests", ServiceName: "api"})
	d.Update(result)
	d.Update(protocol.Focus{Active: false})
	result.Data = client.Envelope[[]apigen.MetricCardinalityStream]{Data: []apigen.MetricCardinalityStream{{Name: "requests", ServiceName: "api", ActiveSeries: 38, Limit: 2000}}}
	d.Update(result)
	if !strings.Contains(d.View(), "38 active series / 2000 limit") {
		t.Fatal("hidden budget did not repair", d.View())
	}
	d.Update(press("w"))
	if !d.admission.loaded {
		t.Fatal("chart window cleared admission scope")
	}
	d.Update(protocol.SessionChanged{ID: "other"})
	d.Update(result)
	if d.admission.loaded {
		t.Fatal("old session budget accepted")
	}
	// Ordinary metrics invalidate the same shared subscription. Catalog-only
	// events and self telemetry must continue to avoid HTTP feedback.
	r := env.Refresh
	for _, w := range r.Due(time.Now()) {
		r.Complete(w.Run(), time.Now())
	}
	r.Observe(liveevent.Message{Type: "metric", Decoded: &liveevent.MetricPayload{CatalogOnly: true}})
	if len(r.Due(time.Now().Add(time.Second))) != 0 {
		t.Fatal("catalog-only event triggered a read")
	}
}
