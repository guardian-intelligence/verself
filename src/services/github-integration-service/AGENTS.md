# github-integration-service

This service is the GitHub boundary for Verself CI. It owns GitHub-specific
business logic and provider truth; it does not own VM leases, Firecracker
snapshots, durable zvol generations, billing windows, or provider-neutral
sandbox execution policy.

## Boundary

- Owns webhook HMAC verification, webhook delivery idempotency,
  workflow/job/run-attempt refresh, GitHub runner registration/JIT config,
  runner assignment, cancellation semantics, provider terminal evidence, and
  GitHub-specific job shape normalization.
- Calls sandbox-rental-service for provider-neutral sandbox primitives:
  execution submission, lease and attempt identity, durable mount plans,
  golden VM activation, GoldenSnapshotBarrier evaluation, checkpoint,
  publishing, and promotion.
- Treats GitHub as provider-truth authority only. The control plane remains the
  snapshot authority because only Verself knows lease identity, durable
  generation identity, trust class, Firecracker compatibility, hook profile, and
  promotion policy.
- Does not call vm-orchestrator directly. All VM and ZFS work goes through
  sandbox-rental-service, which owns the product policy boundary above the
  privileged host daemon.
- Does not store or verify non-retrievable GitHub credential material locally.
  GitHub App private keys, webhook secrets, client secrets, runner bootstrap
  tokens, and rotated provider secrets belong behind secrets-service or a
  service-owned opaque credential reference.

## Security Model

IAM is the customer authorization boundary, not the provider authenticity
boundary.

- Require Verself bearer auth plus IAM for future customer-visible
  configuration mutations: connect/disconnect an installation, sync repository
  access, change runner policy, and customer-initiated cancellation or
  disablement actions. The current production cut keeps onboarding and repo
  management out of this service and uses preconfigured app credentials.
- Require IAM read permissions for customer-visible inventory and diagnostics:
  installation lists, repository lists, runner policy views, and terminal
  evidence views when exposed outside internal service calls.
- Do not use IAM to authenticate GitHub itself. GitHub provider ingress is
  authenticated by provider-specific controls: webhook HMAC for webhooks and
  GitHub API re-reads for terminal job truth.
- Use governance audit events for all externally meaningful state transitions,
  including provider webhook accepted/rejected, runner registration created,
  runner assignment observed, cancellation observed, and terminal job evidence
  emitted.
- Internal calls use SPIFFE mTLS allowlists plus typed Smithy operations. The
  internal permission traits are contract metadata and audit inputs; they are
  not a substitute for peer service identity.

GitHub webhook validation:

- Read and verify the exact raw request body before JSON parsing or enqueueing
  work.
- Require `X-GitHub-Event`, `X-GitHub-Delivery`, and
  `X-Hub-Signature-256`. Reject missing, duplicate, malformed, or legacy
  SHA-1-only signatures.
- Compute HMAC-SHA256 with the installation's configured webhook secret and use
  constant-time comparison against the `sha256=` header value.
- Treat GitHub source IP ranges from the `/meta` API as optional edge
  defense-in-depth only. They are not identity because ranges change and IPv6
  support is evolving.
- Use `X-GitHub-Delivery` as the provider idempotency key. Store every delivery
  outcome so redeliveries collapse deterministically and rejected payloads remain
  visible in telemetry.
- Enforce event and action allowlists. Unknown events are recorded as ignored
  provider evidence; they are not silently dropped.
- Before a webhook can drive sandbox state, reconcile the payload to persisted
  installation/repository/job identity and, for terminal jobs, re-read the exact
  GitHub run/job attempt through the GitHub API.

App onboarding and repository management are intentionally descoped from the
current service surface. When they are added, they must use the standard GitHub
setup callback state machine: mint state only after Verself IAM passes, treat
`installation_id` from the setup URL as spoofable evidence, verify the user and
installation through GitHub APIs, then bind the installation to the Verself org.

Runner and workload bootstrap:

- Generate runner JIT config through GitHub's self-hosted runner API using an
  installation token scoped to the target repository or organization.
- Runner bootstrap tokens are one-time, short-lived, attempt scoped, and
  delivered with `Cache-Control: no-store`.
- Checkout bundles, Bazel telemetry, and any future guest callbacks must
  authenticate against Verself execution/attempt identity, not against ambient
  GitHub runner process state.
- Snapshot promotion must never trust a runner process alone. The input to
  sandbox-rental-service is terminal provider evidence for the exact
  repository, run id, run attempt, job id, runner binding, execution id, attempt
  id, and lease.

## Architecture Highlights

- Smithy is the source of truth for the service data model and interfaces.
  Keep resource identity, provider evidence, runner registration, and sandbox
  binding shapes there instead of repeating schema descriptions in prose.
- Public routes are for GitHub provider webhooks. Customer-visible routes stay
  out until onboarding/repo management is intentionally in scope. Internal
  routes are SPIFFE mTLS only and exist to exchange provider evidence with
  repo-owned services.
- Provider IDs are evidence, not reusable sandbox identity. Use GitHub
  `run_id`, `run_attempt`, `job_id`, repository id, runner id/name, webhook
  delivery id, and observed API timestamps to prove what GitHub said happened.
- Cache compatibility is based on normalized facts, not raw Actions YAML bytes.
  Reordering non-semantic YAML keys must not produce a new job shape.
- Terminal evidence must be exact-attempt scoped. A successful job for the wrong
  `run_attempt`, repository, runner binding, or sandbox execution cannot pass a
  GoldenSnapshotBarrier.
- Cancellation is a first-class provider state. This service translates GitHub
  cancellation and force-cancellation semantics into explicit sandbox commands;
  sandbox-rental-service decides how those commands affect attempts, leases,
  billing, and snapshot promotion.
- ClickHouse events should preserve the provider/control-plane sequence:
  webhook received, provider state refreshed, demand recorded, runner
  registration created, sandbox execution requested, runner assigned, provider
  job terminal, terminal evidence emitted.

## Reference Patterns

GitHub Apps tend to fail in the same few places: webhook trust, setup callback
trust, API budget, runner lifecycle, and replay/idempotency. Borrow these
patterns before inventing service-local variants.

- Fast webhook ingress: validate the raw body signature, persist a delivery
  ledger row with body hash and headers, enqueue background work, and return a
  2XX response quickly. GitHub asks webhook receivers to respond within 10
  seconds and recommends asynchronous processing for work that may take longer.
- Webhooks as hints, API reads as provider truth: a webhook can create demand
  to refresh state, but terminal evidence must come from re-reading the exact
  workflow run/job attempt through GitHub's API before sandbox-rental-service
  receives a promotable result.
- Durable delivery ledger: `X-GitHub-Delivery` is the replay/idempotency key.
  Redeliveries reuse the same delivery id, so store the delivery id, event,
  action, body hash, signature verification result, processing state, and error
  class. A repeated delivery id with a different body hash is a security event.
- Provider reconciler: periodic and demand-driven refreshes must repair missed
  webhooks, out-of-order deliveries, GitHub retries, process restarts, and
  transient GitHub API failures. The reconciler is provider-specific; the
  sandbox service should only see normalized commands and evidence.
- Single GitHub API client boundary: all REST and GraphQL calls go through one
  typed client that owns installation token lookup, token cache expiry, API
  version headers, retries, redirects, conditional requests, pagination, and
  rate-limit telemetry.
- API budget queue: GitHub recommends avoiding concurrent REST requests and
  pausing between mutative requests to avoid secondary rate limits. Model this
  as per-installation and per-repository work queues, not as ad hoc sleeps in
  handlers.
- Installation callback state machine, when introduced: begin installation only
  after Verself IAM allows the actor, mint single-use state, consume that state
  once on callback, exchange the OAuth code, verify the installing user's
  association with the installation, then bind the GitHub installation to the
  Verself org.
- Runner issuance service: create JIT runner config or registration tokens only
  from persisted job demand. Tokens and JIT config are attempt scoped,
  short-lived, redacted in logs, and unusable as snapshot authority.
- Runner assignment is not a deterministic GitHub binding. If multiple queued
  jobs share the same `runs-on` labels, GitHub may assign any matching runner to
  any job. This is a GitHub integration quirk and must not leak into
  sandbox-rental-service. Production JIT issuance is gated per repository runner
  class by a configured active-runner slot limit, never by a global customer
  queue. The retry reconciler must also select distinct repository/class
  candidates instead of replaying one saturated repo as a FIFO. This gate is an
  issuance throttle only; demand that exceeds host capacity will need an
  explicit provider queue later.
- Runner assignment mismatch correction is blocking and local. When GitHub
  reports that runner `R` is executing job `B` but the persisted expectation was
  job `A`, github-integration-service immediately corrects its provider truth
  before forwarding the observation to sandbox-rental-service. If job `B`
  already had another live runner/sandbox assignment, the service performs a
  pairwise control-plane swap of the provider-demand and runner-registration
  rows. If job `B` had no live assignment, it transfers `R` to `B` and returns
  `A` to demand-recorded state with a fresh runner name. The VM is not moved
  underneath a running GitHub runner process; the provider-to-sandbox identity is
  realigned to match the runner GitHub already selected, and sandbox-rental
  binds the actual job by observed runner id/name.
- Assignment mismatches are product signals. Emit
  `github.runner.assignment.mismatch.corrected` with `correction_kind`,
  `assumed_provider_job_id`, `actual_provider_job_id`, and registration state so
  operators can quantify repositories whose workflow labels do not play well
  with warm CI assumptions and notify customers out of band.
- Trust classifier: classify every run/job before it can allocate a sandbox or
  request golden promotion. Forks, pull requests, reusable workflows,
  environment protection, repository visibility, app permission drift, and org
  policy all feed the trust class.
- Provider outbox to sandbox-rental-service: GitHub-specific state changes
  become idempotent provider-neutral commands. The outbox row, not an HTTP
  handler stack frame, is the durable proof that sandbox-rental-service should
  be called.
- ClickHouse wide events: each provider/control-plane transition should emit one
  wide event with stable ids for org, installation, repo, run id, run attempt,
  job id, delivery id, execution id, attempt id, lease id, runner id/name,
  trust class, and decision. Prefer more events over overloading one event with
  hidden phases.

Expected high-level ClickHouse sequence for a successful CI job:

1. `github.webhook.received`
2. `github.webhook.verified`
3. `github.delivery.enqueued`
4. `github.provider.refresh.started`
5. `github.job.demand.recorded`
6. `github.runner.registration.created`
7. `github.sandbox.submit.requested`
8. `github.runner.assignment.observed`
9. `github.runner.assignment.mismatch.corrected` when GitHub selected a
   different compatible runner/job pairing than the service expected
10. `github.job.terminal.observed`
11. `github.terminal_evidence.emitted`
12. `github.golden_snapshot_barrier.requested`

## Security Footguns

- Parsing JSON before verifying the raw-body HMAC lets unauthenticated payloads
  consume CPU, allocate memory, hit logs, or trigger parser edge cases.
- Reconstructing the body before HMAC validation breaks signature semantics.
  The bytes used for HMAC must be the exact bytes GitHub sent.
- Treating IP allowlists, TLS, user agent, or route secrecy as identity is a
  mistake. They are useful defense in depth; webhook HMAC is the provider
  authenticity control.
- Trusting `installation_id` from the setup URL is unsafe. GitHub documents that
  this value can be spoofed, so it must be verified with a user token and an API
  read before binding an installation.
- Treating a webhook terminal payload as promotable terminal truth creates stale
  attempt and replay risks. Always re-read the exact run attempt and job.
- Ignoring `run_attempt` makes retry jobs indistinguishable from the original
  attempt. Snapshot promotion must be exact-attempt scoped.
- Accepting duplicate delivery ids without comparing body hash hides replay or
  storage corruption. Same id plus same body hash is idempotency; same id plus
  different body hash is suspicious.
- Logging webhook bodies, installation tokens, runner registration tokens, JIT
  config, OAuth codes, state values, checkout credentials, or provider secrets
  turns observability into credential exfiltration.
- Letting app permissions drift silently makes failures look like provider
  flakiness. Store expected permission sets and emit explicit drift events.
- Using one global GitHub App credential path for every tenant makes blast radius
  analysis harder. Tenant binding, credential reference, and app installation id
  need to be explicit in every sensitive row and event.
- Letting runner callbacks prove completion leaks GitHub runner process quirks
  into snapshot authority. Runner state is useful evidence, not the promotion
  authority.
- Allowing fork or pull-request workloads to share golden snapshot trust with
  protected branch workloads is a cross-trust cache leak. Trust class must be
  part of job shape and promotion policy.

## Performance Footguns

- Doing synchronous GitHub API fanout inside the webhook request path risks
  timeout redeliveries and duplicate work.
- Polling for everything wastes API budget. Prefer webhook-triggered refreshes
  with a reconciler for missed or ambiguous state.
- Failing to cache installation access tokens creates avoidable JWT signing and
  token-generation load. Installation tokens expire after one hour; cache them
  with expiry and permission/repository scope in the key.
- Running all GitHub API work concurrently invites secondary rate limits.
  Constrain concurrency by installation and repository, and serialize mutative
  calls where GitHub recommends it.
- Retrying rate-limited requests without honoring `retry-after`,
  `x-ratelimit-reset`, and exponential backoff can get the integration banned or
  suspended.
- Manually constructing pagination URLs or parsing GitHub URLs couples us to URL
  shape. Use typed response fields and Link headers.
- Storing large raw webhook bodies in hot PostgreSQL rows bloats critical state.
  Persist the digest and indexed envelope in PostgreSQL; store raw bodies only
  behind a retention-governed blob or append-only evidence path if needed.
- Serializing all repo work behind one global queue wastes parallelism. The
  natural isolation key is installation, repository, workflow run, job, and
  runner binding, with global rate-limit feedback applied above those queues.
- Treating every YAML byte change as a cache bust makes harmless reorderings
  expensive. Normalize the job-shaping fields and hash the canonical model.
- Mixing provider refresh, sandbox command emission, and customer UX projection
  in one transaction makes retries expensive and failure modes unclear. Keep
  provider state, outbox, and read projections separate.

## Implementation Invariants

- Every externally supplied GitHub identifier is evidence until reconciled to a
  persisted installation/repository binding owned by a Verself org.
- Every provider event has an idempotency key and a body or state digest.
- Every sandbox command includes exact provider attempt identity and the
  sandbox execution/attempt binding it applies to.
- Every terminal evidence row records how it was verified: webhook-only,
  API-read, runner-observed, or reconciler-observed. Only API-read exact attempt
  evidence can request golden promotion.
- Every credential-bearing operation returns redacted DTOs and emits redacted
  ClickHouse events by construction.
- Every customer-visible mutation is authorized by IAM before GitHub side
  effects occur.
- Every internal command path uses SPIFFE mTLS and Smithy-modeled operations.
- Every GitHub API call records installation id, endpoint family, HTTP method,
  rate-limit remaining/reset, retry count, request class, and resulting error
  class in ClickHouse.
- Every cache/job-shape hash is generated from a canonical typed model, not raw
  YAML text.

## Open Design Risks

- GitHub.com versus GitHub Enterprise Server needs an explicit product decision.
  GHES changes API base URLs, webhook source ranges, app installation flows,
  version skew, and support boundaries.
- The exact customer-facing IAM permissions for onboarding, repository sync,
  runner policy changes, diagnostics, and cancellation need to be modeled before
  those route handlers ship.
- The secrets-service API for webhook secrets, app private keys, OAuth client
  secrets, and runner bootstrap material needs to be settled before storing any
  credential references.
- The canonical Actions job-shape model needs a narrow field inventory before
  cache key generation ships. Raw workflow bytes should not be the cache key.
- GoldenSnapshotBarrier ownership is push-based: github-integration-service
  pushes exact terminal evidence to sandbox-rental-service over the Smithy
  internal operation, and sandbox-rental-service remains the snapshot authority.

## Editing Notes

- Do not reintroduce GitHub-specific policy into sandbox-rental-service when
  implementing this service. Add a Smithy internal operation or a typed
  service-local client instead.
- Keep webhook parsing and signature verification strict and deterministic.
  Unknown events should be recorded as ignored provider evidence, not silently
  dropped.
- Keep unhappy-path UX explicit. Provider auth failures, stale run attempts,
  missing runner assignment, cancelled jobs, and ambiguous terminal evidence
  need durable state and ClickHouse events.

## Primary References

- GitHub webhook signatures:
  https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries
- GitHub webhook best practices:
  https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks
- GitHub webhook headers and delivery ids:
  https://docs.github.com/en/webhooks/webhook-events-and-payloads
- GitHub REST API best practices:
  https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api
- GitHub setup URL spoofing warning:
  https://docs.github.com/en/enterprise-server@3.19/apps/creating-github-apps/registering-a-github-app/about-the-setup-url
- GitHub App installation tokens:
  https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation
- GitHub self-hosted runner JIT config:
  https://docs.github.com/en/rest/actions/self-hosted-runners
