-- verification_run_id was a bespoke correlation header stamped by the e2e
-- harness alone. It was never read by production code and never used as a
-- filter. W3C tracecontext + fm_correlation_id already cover cross-service
-- request correlation; drop the dedicated column.

ALTER TABLE executions
  DROP COLUMN IF EXISTS verification_run_id;
