-- GORM's default integer timestamp convention is seconds. Alert timestamps
-- are API nanoseconds, so repair rows written before the explicit model tags.
UPDATE alert_rules
SET created_at = created_at * 1000000000
WHERE created_at > 0 AND created_at < 1000000000000;

UPDATE alert_rules
SET updated_at = updated_at * 1000000000
WHERE updated_at > 0 AND updated_at < 1000000000000;
