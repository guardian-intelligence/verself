import { getAccessToken } from "./auth";

async function authFetch(path: string, init?: RequestInit): Promise<Response> {
  const token = await getAccessToken();
  const headers = new Headers(init?.headers);
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  headers.set("Accept", "application/json");
  return fetch(path, { ...init, headers });
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
  return authFetch("/api/v1/billing/subscriptions").then(
    jsonOrThrow<SubscriptionsResponse>,
  );
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
  return authFetch(`/api/v1/billing/grants${qs ? `?${qs}` : ""}`).then(
    jsonOrThrow<GrantsResponse>,
  );
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

export function createSubscription(
  body: SubscribeRequest,
): Promise<{ url: string }> {
  return authFetch("/api/v1/billing/subscribe", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).then(jsonOrThrow<{ url: string }>);
}

// --- Jobs ---

export interface Job {
  id: string;
  org_id: number;
  user_id: string;
  repo_url: string;
  run_command?: string;
  status: string;
  exit_code?: number;
  duration_ms?: number;
  zfs_written?: number;
  started_at?: string;
  completed_at?: string;
  created_at: string;
}

export function submitJob(
  repoURL: string,
  runCommand?: string,
): Promise<{ job_id: string; status: string }> {
  return authFetch("/api/v1/jobs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ repo_url: repoURL, run_command: runCommand }),
  }).then(jsonOrThrow<{ job_id: string; status: string }>);
}

export function fetchJob(jobId: string): Promise<Job> {
  return authFetch(`/api/v1/jobs/${jobId}`).then(jsonOrThrow<Job>);
}

export function fetchJobLogs(
  jobId: string,
): Promise<{ job_id: string; logs: string }> {
  return authFetch(`/api/v1/jobs/${jobId}/logs`).then(
    jsonOrThrow<{ job_id: string; logs: string }>,
  );
}
