ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS last_notified_at BIGINT;
