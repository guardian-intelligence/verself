CREATE TABLE deployment_requests (
  deployment_id text PRIMARY KEY,
  site text NOT NULL,
  sha text NOT NULL,
  deploy_run_key text NOT NULL UNIQUE,
  source_kind text NOT NULL,
  source_subject text NOT NULL DEFAULT '',
  repository text NOT NULL DEFAULT '',
  ref text NOT NULL DEFAULT '',
  workflow text NOT NULL DEFAULT '',
  job_workflow_ref text NOT NULL DEFAULT '',
  provider_run_id text NOT NULL DEFAULT '',
  provider_run_attempt integer NOT NULL DEFAULT 0,
  state text NOT NULL,
  error_code text NOT NULL DEFAULT '',
  error_detail text NOT NULL DEFAULT '',
  nomad_submitted_jobs integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz NOT NULL DEFAULT 'epoch',
  finished_at timestamptz NOT NULL DEFAULT 'epoch'
);

CREATE INDEX deployment_requests_site_created_at_idx
  ON deployment_requests (site, created_at DESC);

CREATE UNIQUE INDEX deployment_requests_github_run_attempt_idx
  ON deployment_requests (site, source_kind, repository, provider_run_id, provider_run_attempt)
  WHERE source_kind = 'github_actions_oidc' AND provider_run_id <> '';

CREATE UNIQUE INDEX deployment_requests_one_active_per_site_idx
  ON deployment_requests (site)
  WHERE state IN ('queued', 'running');

CREATE TABLE deployment_events (
  event_id bigserial PRIMARY KEY,
  deployment_id text NOT NULL REFERENCES deployment_requests(deployment_id) ON DELETE CASCADE,
  at timestamptz NOT NULL DEFAULT now(),
  state text NOT NULL,
  code text NOT NULL,
  detail text NOT NULL DEFAULT ''
);

CREATE INDEX deployment_events_deployment_at_idx
  ON deployment_events (deployment_id, at);
