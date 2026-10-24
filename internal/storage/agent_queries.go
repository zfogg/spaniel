package storage

import (
	"encoding/json"
	"github.com/zfogg/spaniel/internal/model"
)

func encodeAttributeFilters(filters map[string]string) string {
	if len(filters) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(filters)
	return string(b)
}

type AttributeDiscoveryFilter struct {
	SessionID, Service, Signal, Scope, Prefix, Key string
	FromNs, ToNs                                   int64
	Limit, Offset                                  int
}

func (d *DB) DiscoverAttributeKeys(f AttributeDiscoveryFilter) ([]model.AttributeKeyRow, error) {
	return d.query.Span.DiscoverAttributeKeys(f.SessionID, f.Service, f.Signal, f.Scope, f.Prefix, f.FromNs, f.ToNs, f.Limit, f.Offset)
}
func (d *DB) DiscoverAttributeValues(f AttributeDiscoveryFilter) ([]model.AttributeValueRow, error) {
	return d.query.Span.DiscoverAttributeValues(f.SessionID, f.Service, f.Signal, f.Scope, f.Key, f.FromNs, f.ToNs, f.Limit, f.Offset)
}

type ServiceSummaryData struct {
	Overview   model.ServiceOverviewRow
	Operations []model.ServiceOperationRow
	Findings   []model.ServiceFindingRow
	Signals    []model.ServiceSignalRow
}

func (d *DB) GetServiceSummary(sessionID, service string, fromNs, toNs int64, limit int) (ServiceSummaryData, error) {
	out := ServiceSummaryData{}
	rows, err := d.query.Span.ServiceOverview(sessionID, service, fromNs, toNs)
	if err != nil {
		return out, err
	}
	if len(rows) > 0 {
		out.Overview = rows[0]
	}
	out.Operations, err = d.query.Span.ServiceOperations(sessionID, service, fromNs, toNs, limit)
	if err != nil {
		return out, err
	}
	out.Findings, err = d.query.Span.ServiceFindings(sessionID, service, fromNs, toNs, limit)
	if err != nil {
		return out, err
	}
	out.Signals, err = d.query.Span.ServiceSignals(sessionID, service, fromNs, toNs)
	return out, err
}
