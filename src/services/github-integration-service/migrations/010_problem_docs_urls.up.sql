ALTER TABLE github_webhook_deliveries
    ADD COLUMN IF NOT EXISTS primary_problem_docs_url TEXT NOT NULL DEFAULT '';
ALTER TABLE github_provider_demands
    ADD COLUMN IF NOT EXISTS primary_problem_docs_url TEXT NOT NULL DEFAULT '';
ALTER TABLE github_runner_instances
    ADD COLUMN IF NOT EXISTS primary_problem_docs_url TEXT NOT NULL DEFAULT '';
ALTER TABLE github_provider_surface_commands
    ADD COLUMN IF NOT EXISTS primary_problem_docs_url TEXT NOT NULL DEFAULT '';

ALTER TABLE github_webhook_delivery_problems
    ADD COLUMN IF NOT EXISTS docs_url TEXT NOT NULL DEFAULT '';
ALTER TABLE github_provider_demand_problems
    ADD COLUMN IF NOT EXISTS docs_url TEXT NOT NULL DEFAULT '';
ALTER TABLE github_runner_instance_problems
    ADD COLUMN IF NOT EXISTS docs_url TEXT NOT NULL DEFAULT '';
ALTER TABLE github_provider_surface_problems
    ADD COLUMN IF NOT EXISTS docs_url TEXT NOT NULL DEFAULT '';

UPDATE github_webhook_deliveries
SET primary_problem_docs_url = 'https://verself.sh/docs/reference/github-integration/errors#' || replace(replace(primary_problem_code, '.', '-'), '_', '-')
WHERE primary_problem_code <> ''
  AND primary_problem_docs_url = '';

UPDATE github_provider_demands
SET primary_problem_docs_url = 'https://verself.sh/docs/reference/github-integration/errors#' || replace(replace(primary_problem_code, '.', '-'), '_', '-')
WHERE primary_problem_code <> ''
  AND primary_problem_docs_url = '';

UPDATE github_runner_instances
SET primary_problem_docs_url = 'https://verself.sh/docs/reference/github-integration/errors#' || replace(replace(primary_problem_code, '.', '-'), '_', '-')
WHERE primary_problem_code <> ''
  AND primary_problem_docs_url = '';

UPDATE github_provider_surface_commands
SET primary_problem_docs_url = 'https://verself.sh/docs/reference/github-integration/errors#' || replace(replace(primary_problem_code, '.', '-'), '_', '-')
WHERE primary_problem_code <> ''
  AND primary_problem_docs_url = '';

UPDATE github_webhook_delivery_problems
SET docs_url = 'https://verself.sh/docs/reference/github-integration/errors#' || replace(replace(problem_code, '.', '-'), '_', '-')
WHERE problem_code <> ''
  AND docs_url = '';

UPDATE github_provider_demand_problems
SET docs_url = 'https://verself.sh/docs/reference/github-integration/errors#' || replace(replace(problem_code, '.', '-'), '_', '-')
WHERE problem_code <> ''
  AND docs_url = '';

UPDATE github_runner_instance_problems
SET docs_url = 'https://verself.sh/docs/reference/github-integration/errors#' || replace(replace(problem_code, '.', '-'), '_', '-')
WHERE problem_code <> ''
  AND docs_url = '';

UPDATE github_provider_surface_problems
SET docs_url = 'https://verself.sh/docs/reference/github-integration/errors#' || replace(replace(problem_code, '.', '-'), '_', '-')
WHERE problem_code <> ''
  AND docs_url = '';
