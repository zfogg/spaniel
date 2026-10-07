ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS acknowledgement_note TEXT DEFAULT '';
