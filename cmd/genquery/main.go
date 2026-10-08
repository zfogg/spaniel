// Command genquery regenerates Spaniel's typed GORM DAO layer.
//
// It intentionally uses the checked-in storage structs instead of database
// introspection: migrations are the schema authority, and generation must not
// require a writable DuckDB file or a running Spaniel instance.
package main

import (
	"gorm.io/gen"

	"github.com/zfogg/spaniel/internal/model"
)

func main() {
	g := gen.NewGenerator(gen.Config{
		OutPath:      "internal/storage/querygen",
		ModelPkgPath: "github.com/zfogg/spaniel/internal/model",
		Mode:         gen.WithoutContext | gen.WithDefaultQuery | gen.WithQueryInterface,
	})

	g.ApplyBasic(
		model.AlertRule{}, model.AlertInstance{}, model.AlertInstanceTarget{}, model.AlertEvent{}, model.AlertSilence{}, model.NotificationRecord{},
		model.Dashboard{}, model.DashboardVariable{}, model.DashboardPanel{},
		model.Span{}, model.Log{}, model.Session{}, model.LintWarning{},
		model.TraceIssue{}, model.SpanEvent{}, model.SpanLink{}, model.Metric{}, model.Meta{}, model.MetricSeriesCatalog{}, model.CoverageSpec{},
	)
	g.ApplyInterface(func(model.SpanEventMethods) {}, model.SpanEvent{})
	g.ApplyInterface(func(model.SpanLinkMethods) {}, model.SpanLink{})
	g.ApplyInterface(func(model.SpanSearchMethods) {}, model.Span{})
	g.ApplyInterface(func(model.SpanOverlayMethods) {}, model.Span{})
	g.ApplyInterface(func(model.SpanListMethods) {}, model.Span{})
	g.ApplyInterface(func(model.SessionSearchMethods) {}, model.Session{})
	g.ApplyInterface(func(model.SessionMethods) {}, model.Session{})
	g.ApplyInterface(func(model.LogSearchMethods) {}, model.Log{})
	g.ApplyInterface(func(model.MetricMethods) {}, model.Metric{})
	g.ApplyInterface(func(model.SpanMetricMethods) {}, model.Span{})
	g.ApplyInterface(func(model.SpanServiceMapMethods) {}, model.Span{})
	g.ApplyInterface(func(model.SpanStorageMethods) {}, model.Span{})
	g.ApplyInterface(func(model.MetaDiagnosticMethods) {}, model.Meta{})
	g.ApplyInterface(func(model.MetaWriteMethods) {}, model.Meta{})
	g.ApplyInterface(func(model.TraceIssueSearchMethods) {}, model.TraceIssue{})
	g.ApplyInterface(func(model.LintWarningSearchMethods) {}, model.LintWarning{})
	g.ApplyInterface(func(model.LintWarningMethods) {}, model.LintWarning{})
	g.Execute()
}
