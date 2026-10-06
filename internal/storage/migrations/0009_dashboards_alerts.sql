CREATE TABLE IF NOT EXISTS dashboards (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS dashboard_variables (
    dashboard_id TEXT NOT NULL, name TEXT NOT NULL, kind TEXT NOT NULL,
    source TEXT NOT NULL, options_json VARCHAR NOT NULL DEFAULT '[]', default_value TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (dashboard_id, name)
);
CREATE TABLE IF NOT EXISTS dashboard_panels (
    id TEXT PRIMARY KEY, dashboard_id TEXT NOT NULL, title TEXT NOT NULL,
    display_type TEXT NOT NULL, query_text TEXT NOT NULL, query_json VARCHAR NOT NULL,
    settings_json VARCHAR NOT NULL DEFAULT '{}', layout_json VARCHAR NOT NULL DEFAULT '{}',
    position INTEGER NOT NULL, updated_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_dashboard_panels_order ON dashboard_panels(dashboard_id, position);
CREATE INDEX IF NOT EXISTS idx_dashboard_variables_dashboard ON dashboard_variables(dashboard_id);
CREATE TABLE IF NOT EXISTS alert_rules (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, query_json VARCHAR NOT NULL,
    condition_json VARCHAR NOT NULL, group_by_json VARCHAR NOT NULL DEFAULT '[]',
    pending_for_ns BIGINT NOT NULL DEFAULT 0, cooldown_ns BIGINT NOT NULL DEFAULT 0,
    severity TEXT NOT NULL DEFAULT 'warning', annotations_json VARCHAR NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE, created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS alert_instances (
    rule_id TEXT NOT NULL, group_key TEXT NOT NULL, labels_json VARCHAR NOT NULL,
    state TEXT NOT NULL, value DOUBLE, first_pending_at BIGINT, fired_at BIGINT,
    resolved_at BIGINT, acknowledged_at BIGINT, last_notified_at BIGINT, last_evaluated_at BIGINT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '', PRIMARY KEY (rule_id, group_key)
);
CREATE INDEX IF NOT EXISTS idx_alert_instances_state ON alert_instances(state, last_evaluated_at);
