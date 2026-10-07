ALTER TABLE alert_silences ADD COLUMN IF NOT EXISTS group_key TEXT DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_alert_silences_rule_group_window ON alert_silences(rule_id, group_key, starts_at, ends_at);
