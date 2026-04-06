CREATE SEQUENCE IF NOT EXISTS job_billing_id_seq AS BIGINT;

ALTER TABLE jobs
  ADD COLUMN IF NOT EXISTS billing_job_id BIGINT;

ALTER TABLE jobs
  ADD COLUMN IF NOT EXISTS billing_reservation JSONB;

UPDATE jobs
SET billing_reservation = NULL
WHERE billing_reservation IS DISTINCT FROM NULL;

ALTER TABLE jobs
  DROP COLUMN IF EXISTS billing_reservation_id;
