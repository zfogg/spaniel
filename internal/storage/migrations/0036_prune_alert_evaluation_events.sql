-- Per-pass evaluation rows flood grouped-alert history without conveying a
-- transition. Evaluation health stays on alert_rules; history is lifecycle and
-- delivery oriented.
DELETE FROM alert_events WHERE kind = 'evaluation';
