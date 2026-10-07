ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS instance_discovery_sql TEXT DEFAULT '';
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS instance_discovery_interval_ns BIGINT DEFAULT 0;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS instance_discovery_stale_after_ns BIGINT DEFAULT 0;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS instance_discovery_last_run_at BIGINT DEFAULT 0;

CREATE TABLE IF NOT EXISTS alert_instance_targets (
    rule_id TEXT NOT NULL,
    group_key TEXT NOT NULL,
    labels_json TEXT NOT NULL,
    discovered_at BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL,
    PRIMARY KEY (rule_id, group_key)
);
CREATE INDEX IF NOT EXISTS idx_alert_instance_targets_rule_seen
    ON alert_instance_targets(rule_id, last_seen_at DESC);
