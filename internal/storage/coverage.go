package storage

import "github.com/zfogg/spaniel/internal/model"

type CoverageOperation = model.CoverageOperation

type CoverageQuality = model.CoverageQuality

type CoverageSpec = model.CoverageSpec

func (d *DB) ListCoverageSpecs() ([]*CoverageSpec, error) {
	return d.query.CoverageSpec.Order(d.query.CoverageSpec.CreatedAt.Desc()).Find()
}

func (d *DB) GetCoverageSpec(id string) (*CoverageSpec, error) {
	return d.query.CoverageSpec.Where(d.query.CoverageSpec.ID.Eq(id)).First()
}

func (d *DB) CreateCoverageSpec(spec *CoverageSpec) error {
	return d.query.CoverageSpec.Create(spec)
}

func (d *DB) UpdateCoverageSpec(spec *CoverageSpec) error {
	q := d.query.CoverageSpec
	// DuckDB cannot rewrite indexed columns as part of a generated model-wide
	// update. A coverage-spec refresh keeps its ID and telemetry service, so
	// update only the mutable document metadata and body.
	_, err := q.Where(q.ID.Eq(spec.ID)).UpdateSimple(
		q.Name.Value(spec.Name),
		q.Format.Value(spec.Format),
		q.SourceURL.Value(spec.SourceURL),
		q.Content.Value(spec.Content),
		q.Digest.Value(spec.Digest),
		q.RouteCount.Value(spec.RouteCount),
		q.Enabled.Value(spec.Enabled),
	)
	return err
}

func (d *DB) DeleteCoverageSpec(id string) error {
	_, err := d.query.CoverageSpec.Where(d.query.CoverageSpec.ID.Eq(id)).Delete()
	return err
}

// ListCoverageOperations returns one row per observed operation in a session.
// Route extraction and percentile aggregation stay in DuckDB so coverage does
// not need to materialize the complete session in memory.
func (d *DB) ListCoverageOperations(sessionID string) ([]*CoverageOperation, error) {
	rows, err := d.namedQuery("storage.ListCoverageOperations").Span.ListCoverageOperations(sessionID)
	if err != nil {
		return nil, err
	}
	operations := make([]*CoverageOperation, len(rows))
	for i := range rows {
		operations[i] = &rows[i]
	}
	return operations, nil
}

func (d *DB) GetCoverageQuality(sessionID string) (*CoverageQuality, error) {
	rows, err := d.namedQuery("storage.GetCoverageQuality").Span.GetCoverageQuality(sessionID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return &CoverageQuality{}, nil
	}
	return &rows[0], nil
}
