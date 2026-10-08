CREATE TABLE IF NOT EXISTS coverage_specs (
  id VARCHAR PRIMARY KEY,
  name VARCHAR NOT NULL,
  service_name VARCHAR NOT NULL,
  format VARCHAR NOT NULL,
  source_url VARCHAR NOT NULL DEFAULT '',
  content VARCHAR NOT NULL,
  digest VARCHAR NOT NULL,
  route_count INTEGER NOT NULL DEFAULT 0,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_coverage_specs_service ON coverage_specs(service_name);
