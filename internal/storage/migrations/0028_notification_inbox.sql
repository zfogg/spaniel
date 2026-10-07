CREATE TABLE IF NOT EXISTS notification_records (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL,
  source_id TEXT NOT NULL,
  severity TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  link TEXT NOT NULL DEFAULT '',
  dedupe_key TEXT NOT NULL,
  created_at BIGINT NOT NULL,
  read_at BIGINT,
  acknowledged_at BIGINT
);
CREATE INDEX IF NOT EXISTS idx_notification_records_time ON notification_records(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_records_dedupe ON notification_records(dedupe_key, created_at DESC);
