-- Add scope attributes without rewriting retained lossless metric points.
ALTER TABLE metrics ADD COLUMN IF NOT EXISTS scope_attributes VARCHAR;
