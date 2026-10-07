ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS last_browser_notified_at BIGINT;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS last_pushover_notified_at BIGINT;

-- Preserve the pre-channel delivery marker as an initial successful timestamp
-- for existing instances. New deliveries track each destination separately.
UPDATE alert_instances
SET last_browser_notified_at = last_notified_at
WHERE last_browser_notified_at IS NULL AND last_notified_at IS NOT NULL;
