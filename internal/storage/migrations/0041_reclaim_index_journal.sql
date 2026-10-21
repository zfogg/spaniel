-- Keep immutable index definitions separate from their small active flag.
-- No ART index: recovery bookkeeping must not retain indexed tombstones.
CREATE TABLE IF NOT EXISTS retention_index_journal (
    table_name VARCHAR,
    plan_json VARCHAR,
    active BOOLEAN DEFAULT FALSE
);
