# GitHub App Onboarding Architecture

`github-integration-service` owns GitHub provider identity, installation truth,
repository access, webhook delivery truth, GitHub API calls, and Actions runner
registration. Verself IAM owns the tenant authorization boundary. GitHub facts
are provider evidence; they never grant Verself permissions by themselves.

The onboarding model separates four concerns:

- Provider truth: GitHub accounts, installations, installation repository
  access, repository metadata, permissions, provider events, and API
  observations.
- Tenant ownership: Verself organization membership, IAM decisions, audit
  activity, and non-billable control-plane setup state.
- Product enablement: installation bindings, repository bindings, runner
  policy, trust policy, and golden snapshot eligibility.
- Runtime evidence: workflow runs, workflow jobs, runner registrations,
  sandbox execution bindings, terminal evidence, and golden barrier inputs.

Provider rows do not authorize runtime. Runtime must resolve an active
`github_installation_binding`, selected provider repository access in
`github_installation_repositories`, and an enabled
`github_repository_binding`.

## Entity Graph

```text
iam org
  -> github_installation_binding
       -> github_installation
            -> github_account
            -> github_installation_repository
                 -> github_repository
       -> github_repository_binding
            -> github_repository

iam actor
  -> github_user_authorization

github_setup_session
  -> iam org
  -> iam actor
  -> github_user_authorization
  -> github_installation_binding
```

One GitHub App installation can have one active Verself organization binding.
A Verself organization can bind many GitHub organization installations and
personal-account installations. Multiple Verself users can authorize their own
GitHub user accounts to complete setup for the same Verself organization.

## Persistence

### Provider Truth

`github_accounts` stores GitHub account display state:
`provider_account_id`, `login`, `account_type`, `avatar_url`, `html_url`,
`state`, `last_event_delivery_id`, and `observed_from_api_at`.

`github_installations` stores provider installation state:
`provider_installation_id`, `provider_account_id`, `account_login`,
`account_type`, `app_slug`, `target_type`, `repository_selection`,
`configuration_url`, `permissions_json`, suspension metadata, provider state,
last delivery, and API observation time.

`github_repositories` stores provider repository state independent from
product enablement: `provider_repository_id`, `provider_account_id`,
`owner_login`, `repository_name`, `repository_full_name`, `default_branch`,
`private`, `archived`, `visibility`, `html_url`, provider state, last delivery,
and API observation time.

`github_installation_repositories` stores current provider access for one
installation and one repository. State values are `selected` and `removed` in
the initial cut. Selected repository changes are written from
`installation_repositories` webhooks and manual provider sync.

### Tenant Bindings

`github_setup_sessions` is the single-use GitHub setup callback state machine.
It stores only the hashed GitHub `state` value, the selected Verself org, the
actor, generated installation URL, optional callback URL, expiry, and completion
links to the user authorization and installation binding.

`github_oauth_sessions` is the single-use GitHub user authorization state
machine. It stores only the hashed OAuth `state`, generated authorization URL,
callback URL, expiry, and completion link to `github_user_authorizations`.

`github_user_authorizations` stores a Verself actor's GitHub App user
authorization metadata: GitHub user id/login, scopes, state, timestamps, and an
opaque `credential_ref`. The user token itself is stored in secrets-service.
Runtime uses installation tokens, not user tokens.

`github_installation_bindings` stores tenant ownership for a GitHub
installation. The active uniqueness constraint is on
`provider_installation_id`, so the same provider installation cannot be
actively bound to two Verself organizations.

`github_repository_bindings` stores product enablement for one repository under
an installation binding. Enabling a repository requires selected provider
access and an active installation binding. Disabling a repository stops new
runtime demand without modifying GitHub installation access.

`github_idempotency_records` stores mutation replay evidence keyed by
`org_id`, `actor_id`, Smithy operation id, and hashed idempotency key. It stores
a request hash and response JSON. Reusing the same key with a different request
hash returns an idempotency mismatch without touching GitHub.

`github_provider_reconciliations` records manual and worker-driven provider
sync attempts with scope ids, reason, state, retry metadata, and failure
reason.

### Runtime Tables

The runtime tables retain GitHub workflow and runner truth:
`github_webhook_deliveries`, `github_workflow_runs`, `github_workflow_jobs`,
`github_job_shapes`, `github_provider_demands`,
`github_runner_registrations`, `github_provider_outbox`,
`github_terminal_job_evidence`, and `github_golden_snapshot_barriers`.

Runtime rows carry `org_id`, `installation_binding_id`, and
`repository_binding_id` when a workflow job resolves through active bindings.
Webhook jobs for unbound or disabled repositories are recorded and ignored for
sandbox submission.

## Customer API

Customer operations use Zitadel bearer auth, org-scoped IAM from the token, the
Smithy-modeled route catalog, idempotency for mutations, fixed-window API rate
limits, and ClickHouse evidence.

| Operation | HTTP shape | Permission |
| --- | --- | --- |
| `StartGithubAppSetup` | `POST /api/v1/github/setup-sessions` | `github:installation:write` |
| `GetGithubSetupSession` | `GET /api/v1/github/setup-sessions/{setup_session_id}` | `github:installation:read` |
| `CompleteGithubAppSetup` | `POST /api/v1/github/setup-sessions/{setup_session_id}/complete` | `github:installation:write` |
| `StartGithubUserAuthorization` | `POST /api/v1/github/user-authorizations` | `github:user_authorization:write` |
| `CompleteGithubUserAuthorization` | `POST /api/v1/github/user-authorizations:complete` | `github:user_authorization:write` |
| `ListGithubInstallations` | `GET /api/v1/github/installations` | `github:installation:read` |
| `GetGithubInstallation` | `GET /api/v1/github/installations/{installation_binding_id}` | `github:installation:read` |
| `SyncGithubInstallation` | `POST /api/v1/github/installations/{installation_binding_id}/sync` | `github:installation:write` |
| `DisconnectGithubInstallation` | `DELETE /api/v1/github/installations/{installation_binding_id}` | `github:installation:write` |
| `ListGithubRepositories` | `GET /api/v1/github/repositories?installation_binding_id=...` | `github:repository:read` |
| `GetGithubRepository` | `GET /api/v1/github/repositories/{repository_binding_id}` | `github:repository:read` |
| `EnableGithubRepository` | `POST /api/v1/github/repositories:enable` | `github:repository:write` |
| `DisableGithubRepository` | `POST /api/v1/github/repositories/{repository_binding_id}/disable` | `github:repository:write` |

List operations use cursor tokens from the first version. Mutation request
bodies carry `idempotency_key` except `DisconnectGithubInstallation`, which
uses the `Idempotency-Key` header.

## Setup Flow

```text
Verself UI or CLI
  -> StartGithubAppSetup
  -> redirect to GitHub install URL with state
  -> GitHub returns setup state and installation_id to Verself facade
  -> facade calls CompleteGithubAppSetup
  -> if a GitHub user token is required:
       StartGithubUserAuthorization
       CompleteGithubUserAuthorization
       CompleteGithubAppSetup
  -> service verifies installation access through GitHub APIs
  -> service creates installation binding and syncs selected repositories
```

The setup URL `installation_id` is treated as spoofable evidence. Completion
verifies that the GitHub user token can list the installation and then reads the
installation through GitHub App authentication before activating a binding.

If the user returns from GitHub without a Verself session, the facade should
show an authentication-required state and resume the existing setup session
after login. Organization switching is explicit because setup sessions are
bound to the org selected before redirect.

## Provider Ingress

`ReceiveGithubWebhook` remains the public provider ingress:

```text
POST /api/v1/github/webhooks
```

The service verifies the exact raw-body HMAC, records the delivery id and body
hash, then workers process supported events:

- `workflow_job` for runtime demand and terminal evidence refresh.
- `installation` for installation lifecycle and provider revocation.
- `installation_repositories` for selected repository additions/removals.
- `github_app_authorization` for GitHub user authorization revocation.
- `repository` for delete, transfer, archive, and metadata changes.

Provider deletion or suspension marks active installation bindings revoked and
blocks runtime. Selected repository removal marks enabled repository bindings
unavailable. GitHub user authorization revocation only revokes setup/UX
credentials; runtime remains controlled by installation and repository binding
state.

## Secrets

The service reads GitHub App private key, webhook secret, and OAuth client
secret from secrets-service runtime secrets. GitHub user tokens are written to
secrets-service under the service-owned user-token prefix. PostgreSQL stores
only credential references and metadata.

Installation tokens are generated through GitHub App authentication, cached in
memory by installation id, and expired before GitHub's expiry. They are not
stored in PostgreSQL or ClickHouse.

## Observability

`verself.github_integration_events` records onboarding, provider API, webhook,
runtime demand, runner, sandbox submission, and terminal evidence events.
Rows include `org_id`, `installation_binding_id`, `repository_binding_id`,
provider ids, repository full name, runner ids, sandbox ids, trace id, span id,
and structured JSON attributes.

The expected release evidence for this surface is live evidence:
PostgreSQL rows for setup/bindings/idempotency, ClickHouse rows for each
operation path exercised, and service traces that join bearer-auth API calls,
GitHub provider calls, webhook processing, and runtime gating.
