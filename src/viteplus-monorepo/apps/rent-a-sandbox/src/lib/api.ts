import { AuthenticationRequiredError, clearUser, getAccessToken, signIn } from "./auth";
import { correlationCookieName, correlationHeaderName, readBrowserCookie } from "./correlation";

async function requireAuthentication(): Promise<never> {
  await clearUser();
  if (typeof window !== "undefined") {
    // Protected product APIs must fail closed. Redirect only the same-origin app
    // shell, never the cross-origin OIDC discovery traffic.
    window.setTimeout(() => {
      void signIn();
    }, 0);
  }
  throw new AuthenticationRequiredError();
}

async function authFetch(path: string, init?: RequestInit): Promise<Response> {
  const token = await getAccessToken();
  const protectedAPI = path.startsWith("/api/v1/");
  if (protectedAPI && !token) {
    return requireAuthentication();
  }

  const headers = new Headers(init?.headers);
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  headers.set("Accept", "application/json");
  if (protectedAPI) {
    const correlationID = readBrowserCookie(correlationCookieName);
    if (correlationID) {
      // Keep correlation on same-origin product APIs only. Cross-origin OIDC
      // traffic must not inherit custom headers or it will preflight.
      headers.set(correlationHeaderName, correlationID);
    }
  }
  const resp = await fetch(path, { ...init, headers });
  if (protectedAPI && resp.status === 401) {
    return requireAuthentication();
  }
  return resp;
}

async function jsonOrThrow<T>(resp: Response): Promise<T> {
  if (!resp.ok) {
    const body = await resp.text().catch(() => "");
    throw new Error(`API ${resp.status}: ${body}`);
  }
  return resp.json();
}

// --- Balance ---

export interface Balance {
  org_id: string;
  free_tier_available: number;
  free_tier_pending: number;
  credit_available: number;
  credit_pending: number;
  total_available: number;
}

export function fetchBalance(): Promise<Balance> {
  return authFetch("/api/v1/billing/balance").then(jsonOrThrow<Balance>);
}

// --- Subscriptions ---

export interface Subscription {
  subscription_id: number;
  plan_id: string;
  product_id: string;
  cadence: string;
  status: string;
  stripe_subscription_id: string;
  current_period_start: string;
  current_period_end: string;
  overage_cap_units: number;
  created_at: string;
}

export interface SubscriptionsResponse {
  org_id: string;
  subscriptions: Subscription[] | null;
}

export function fetchSubscriptions(): Promise<SubscriptionsResponse> {
  return authFetch("/api/v1/billing/subscriptions").then(jsonOrThrow<SubscriptionsResponse>);
}

// --- Grants ---

export interface Grant {
  grant_id: string;
  product_id: string;
  amount: number;
  source: string;
  expires_at: string | null;
  closed_at: string | null;
  created_at: string;
}

export interface GrantsResponse {
  org_id: string;
  grants: Grant[] | null;
}

export function fetchGrants(active?: boolean): Promise<GrantsResponse> {
  const params = new URLSearchParams();
  if (active !== undefined) params.set("active", String(active));
  const qs = params.toString();
  return authFetch(`/api/v1/billing/grants${qs ? `?${qs}` : ""}`).then(jsonOrThrow<GrantsResponse>);
}

// --- Checkout ---

export interface CheckoutRequest {
  product_id: string;
  amount_cents: number;
  success_url: string;
  cancel_url: string;
}

export function createCheckout(body: CheckoutRequest): Promise<{ url: string }> {
  return authFetch("/api/v1/billing/checkout", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).then(jsonOrThrow<{ url: string }>);
}

// --- Subscribe ---

export interface SubscribeRequest {
  plan_id: string;
  cadence?: "monthly" | "annual";
  success_url: string;
  cancel_url: string;
}

export function createSubscription(body: SubscribeRequest): Promise<{ url: string }> {
  return authFetch("/api/v1/billing/subscribe", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).then(jsonOrThrow<{ url: string }>);
}

// --- Executions ---

export interface Attempt {
  attempt_id: string;
  attempt_seq: number;
  state: string;
  orchestrator_job_id?: string;
  billing_job_id?: number;
  runner_name?: string;
  golden_snapshot?: string;
  failure_reason?: string;
  exit_code?: number;
  duration_ms?: number;
  zfs_written?: number;
  stdout_bytes?: number;
  stderr_bytes?: number;
  trace_id?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface BillingWindow {
  attempt_id: string;
  window_seq: number;
  window_seconds: number;
  actual_seconds?: number;
  pricing_phase?: string;
  state: string;
  created_at: string;
  settled_at?: string;
}

export interface Execution {
  execution_id: string;
  org_id: number;
  actor_id: string;
  kind: string;
  provider?: string;
  product_id: string;
  status: string;
  idempotency_key?: string;
  repo_id?: string;
  golden_generation_id?: string;
  repo?: string;
  repo_url?: string;
  ref?: string;
  default_branch?: string;
  run_command?: string;
  commit_sha?: string;
  workflow_path?: string;
  workflow_job_name?: string;
  provider_run_id?: string;
  provider_job_id?: string;
  latest_attempt: Attempt;
  billing_windows?: BillingWindow[];
  created_at: string;
  updated_at: string;
}

export interface RepoExecutionRequest {
  repo_id?: string;
  repo_url?: string;
  ref?: string;
}

export function submitRepoExecution(
  body: RepoExecutionRequest,
): Promise<{ execution_id: string; attempt_id: string; status: string }> {
  const payload: Record<string, string> = {
    kind: "repo_exec",
  };
  if (body.repo_id) payload.repo_id = body.repo_id;
  if (body.repo_url) payload.repo_url = body.repo_url;
  if (body.ref) payload.ref = body.ref;
  return authFetch("/api/v1/executions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  }).then(jsonOrThrow<{ execution_id: string; attempt_id: string; status: string }>);
}

export function fetchExecution(executionId: string): Promise<Execution> {
  return authFetch(`/api/v1/executions/${executionId}`).then(jsonOrThrow<Execution>);
}

export function fetchExecutionLogs(
  executionId: string,
): Promise<{ execution_id: string; attempt_id: string; logs: string }> {
  return authFetch(`/api/v1/executions/${executionId}/logs`).then(
    jsonOrThrow<{ execution_id: string; attempt_id: string; logs: string }>,
  );
}

// --- Repos ---

export interface WorkflowScanIssue {
  path: string;
  job_id?: string;
  reason: string;
  labels?: string[];
  details?: string;
}

export interface RepoCompatibilitySummary {
  workflow_paths?: string[];
  unsupported_labels?: string[];
  issues?: WorkflowScanIssue[];
}

export interface Repo {
  repo_id: string;
  org_id: number;
  provider: string;
  provider_repo_id: string;
  owner: string;
  name: string;
  full_name: string;
  clone_url: string;
  default_branch: string;
  runner_profile_slug: string;
  state: string;
  compatibility_status: string;
  compatibility_summary?: RepoCompatibilitySummary;
  last_scanned_sha?: string;
  active_golden_generation_id?: string;
  last_ready_sha?: string;
  last_error?: string;
  created_at: string;
  updated_at: string;
  archived_at?: string;
}

export interface GoldenGeneration {
  golden_generation_id: string;
  repo_id: string;
  runner_profile_slug: string;
  source_ref: string;
  source_sha: string;
  state: string;
  trigger_reason: string;
  execution_id?: string;
  attempt_id?: string;
  orchestrator_job_id?: string;
  snapshot_ref?: string;
  activated_at?: string;
  superseded_at?: string;
  failure_reason?: string;
  failure_detail?: string;
  created_at: string;
  updated_at: string;
}

export interface RepoBootstrapRecord {
  repo: Repo;
  generation: GoldenGeneration;
  execution_id: string;
  attempt_id: string;
  trigger_reason: string;
}

export interface ImportRepoRequest {
  provider?: string;
  provider_repo_id?: string;
  owner?: string;
  name?: string;
  full_name?: string;
  clone_url: string;
  default_branch?: string;
}

export function importRepo(body: ImportRepoRequest): Promise<Repo> {
  return authFetch("/api/v1/repos", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).then(jsonOrThrow<Repo>);
}

export function fetchRepos(): Promise<Repo[]> {
  return authFetch("/api/v1/repos").then(jsonOrThrow<Repo[]>);
}

export function fetchRepo(repoId: string): Promise<Repo> {
  return authFetch(`/api/v1/repos/${repoId}`).then(jsonOrThrow<Repo>);
}

export function rescanRepo(repoId: string): Promise<Repo> {
  return authFetch(`/api/v1/repos/${repoId}/rescan`, {
    method: "POST",
  }).then(jsonOrThrow<Repo>);
}

export function fetchRepoGenerations(repoId: string): Promise<GoldenGeneration[]> {
  return authFetch(`/api/v1/repos/${repoId}/generations`).then(jsonOrThrow<GoldenGeneration[]>);
}

export function refreshRepo(repoId: string): Promise<RepoBootstrapRecord> {
  return authFetch(`/api/v1/repos/${repoId}/refresh`, {
    method: "POST",
  }).then(jsonOrThrow<RepoBootstrapRecord>);
}
