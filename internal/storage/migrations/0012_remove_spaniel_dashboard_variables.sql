-- Dashboard query parameters must describe the user's telemetry, never
-- Spaniel's own collector instrumentation.
DELETE FROM dashboard_variables
WHERE lower(name) LIKE 'spaniel.%'
   OR lower(source) LIKE 'spaniel.%';
