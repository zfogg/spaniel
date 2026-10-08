package storage

import (
	"fmt"

	"github.com/zfogg/spaniel/internal/model"
)

type ServiceMapNode = model.ServiceMapNode

type ServiceMapOpStat = model.ServiceMapOpStat

type ServiceMapEdge = model.ServiceMapEdge

type ServiceMapData struct {
	Nodes []*ServiceMapNode `json:"nodes"`
	Edges []*ServiceMapEdge `json:"edges"`
}

// ListServices returns distinct service names. An empty sessionID returns
// services across all sessions; a non-empty one scopes to that session.
func (d *DB) ListServices(sessionID string) ([]string, error) {
	var result []string
	q := d.query.Span.Distinct(d.query.Span.ServiceName).Order(d.query.Span.ServiceName)
	if sessionID != "" {
		q = q.Where(d.query.Span.SessionID.Eq(sessionID))
	}
	err := q.Pluck(d.query.Span.ServiceName, &result)
	return result, err
}

func (d *DB) GetServiceMap(sessionID string) (*ServiceMapData, error) {
	nodeRows, err := d.query.Span.ListServiceMapNodes(sessionID)
	if err != nil {
		return nil, fmt.Errorf("service map nodes: %w", err)
	}
	nodes := make([]*ServiceMapNode, len(nodeRows))
	for i := range nodeRows {
		nodes[i] = &nodeRows[i]
	}

	// Top operations per service for the inspector panel.
	for _, n := range nodes {
		ops, err := d.topOperationsForService(n.ID, sessionID, 5)
		if err != nil {
			return nil, fmt.Errorf("top ops for %s: %w", n.ID, err)
		}
		n.TopOps = ops
	}

	edgeRows, err := d.query.Span.ListServiceMapEdges(sessionID)
	if err != nil {
		return nil, fmt.Errorf("service map edges: %w", err)
	}
	edges := make([]*ServiceMapEdge, len(edgeRows))
	for i := range edgeRows {
		edges[i] = &edgeRows[i]
	}

	if nodes == nil {
		nodes = []*ServiceMapNode{}
	}
	if edges == nil {
		edges = []*ServiceMapEdge{}
	}
	return &ServiceMapData{Nodes: nodes, Edges: edges}, nil
}

// topOperationsForService returns the N most-common (service, name) pairs for
// the inspector panel, with per-op p95 latency.
func (d *DB) topOperationsForService(service, sessionID string, limit int) ([]ServiceMapOpStat, error) {
	return d.query.Span.ListTopOperations(service, sessionID, limit)
}

// GetServiceP95 returns the p95 duration_ns for spans of the given service.
// Returns 0 (no error) when there are no stored spans for that service yet.
func (d *DB) GetServiceP95(serviceName string) (int64, error) {
	rows, err := d.query.Span.ServiceP95(serviceName)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].P95Ns, nil
}
