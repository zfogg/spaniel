-- Serial database spans do not prove that an application performed blocking
-- synchronous I/O. Remove findings emitted by the retired heuristic.
DELETE FROM trace_issues WHERE kind = 'synchronous_io';
