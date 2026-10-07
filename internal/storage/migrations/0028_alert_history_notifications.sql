ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS browser_enabled BOOLEAN;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS pushover_enabled BOOLEAN;
UPDATE alert_rules SET browser_enabled = TRUE WHERE browser_enabled IS NULL;
UPDATE alert_rules SET pushover_enabled = TRUE WHERE pushover_enabled IS NULL;
CREATE TABLE IF NOT EXISTS alert_events (id TEXT PRIMARY KEY, rule_id TEXT NOT NULL, group_key TEXT NOT NULL, kind TEXT NOT NULL, state TEXT NOT NULL, value DOUBLE, detail TEXT NOT NULL DEFAULT '', created_at BIGINT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_alert_events_rule_group_time ON alert_events(rule_id, group_key, created_at DESC);
CREATE TABLE IF NOT EXISTS alert_silences (id TEXT PRIMARY KEY, rule_id TEXT NOT NULL, comment TEXT NOT NULL DEFAULT '', starts_at BIGINT NOT NULL, ends_at BIGINT NOT NULL, created_at BIGINT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_alert_silences_rule_window ON alert_silences(rule_id, starts_at, ends_at);
