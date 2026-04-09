import { createServerFn } from "@tanstack/react-start";
import { requireURLFromEnv } from "@forge-metal/web-env";
import { rentASandboxAuthMiddleware, verificationRunMiddleware } from "./auth";

const SANDBOX_RENTAL_SERVICE_BASE_URL = requireURLFromEnv("SANDBOX_RENTAL_SERVICE_BASE_URL");
const verificationRunHeader = "X-Forge-Metal-Verification-Run";

async function sandboxRentalServiceRequest<T>(
  accessToken: string,
  path: string,
  init?: RequestInit,
  verificationRunID?: string,
): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Accept", "application/json");
  headers.set("Authorization", `Bearer ${accessToken}`);
  if (verificationRunID) {
    headers.set(verificationRunHeader, verificationRunID);
  }
  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(new URL(path, SANDBOX_RENTAL_SERVICE_BASE_URL), {
    ...init,
    headers,
  });

  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new Error(`Sandbox rental API ${response.status}: ${body}`);
  }

  return response.json() as Promise<T>;
}

export interface Balance {
  org_id: string;
  free_tier_available: number;
  free_tier_pending: number;
  credit_available: number;
  credit_pending: number;
  total_available: number;
}

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

export interface CheckoutRequest {
  product_id: string;
  amount_cents: number;
  success_url: string;
  cancel_url: string;
}

export interface SubscribeRequest {
  plan_id: string;
  cadence?: "monthly" | "annual";
  success_url: string;
  cancel_url: string;
}

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

export const getBalance = createServerFn({ method: "GET" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return sandboxRentalServiceRequest<Balance>(
      context.auth.accessToken,
      "/api/v1/billing/balance",
      undefined,
      context.verificationRunID,
    );
  });

export const getSubscriptions = createServerFn({ method: "GET" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return sandboxRentalServiceRequest<SubscriptionsResponse>(
      context.auth.accessToken,
      "/api/v1/billing/subscriptions",
      undefined,
      context.verificationRunID,
    );
  });

export const getGrants = createServerFn({ method: "GET" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .inputValidator((data: { active?: boolean; productId?: string } = {}) => data)
  .handler(async ({ context, data }) => {
    const searchParams = new URLSearchParams();
    if (data.active !== undefined) {
      searchParams.set("active", String(data.active));
    }
    if (data.productId) {
      searchParams.set("product_id", data.productId);
    }
    const search = searchParams.toString();
    const path = search ? `/api/v1/billing/grants?${search}` : "/api/v1/billing/grants";
    return sandboxRentalServiceRequest<GrantsResponse>(
      context.auth.accessToken,
      path,
      undefined,
      context.verificationRunID,
    );
  });

export const createCheckoutSession = createServerFn({ method: "POST" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .inputValidator((data: CheckoutRequest) => data)
  .handler(async ({ context, data }) => {
    return sandboxRentalServiceRequest<{ url: string }>(
      context.auth.accessToken,
      "/api/v1/billing/checkout",
      {
        method: "POST",
        body: JSON.stringify(data),
      },
      context.verificationRunID,
    );
  });

export const createSubscriptionSession = createServerFn({ method: "POST" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .inputValidator((data: SubscribeRequest) => data)
  .handler(async ({ context, data }) => {
    return sandboxRentalServiceRequest<{ url: string }>(
      context.auth.accessToken,
      "/api/v1/billing/subscribe",
      {
        method: "POST",
        body: JSON.stringify(data),
      },
      context.verificationRunID,
    );
  });

export const submitRepoExecution = createServerFn({ method: "POST" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .inputValidator((data: RepoExecutionRequest) => data)
  .handler(async ({ context, data }) => {
    return sandboxRentalServiceRequest<{ execution_id: string; attempt_id: string; status: string }>(
      context.auth.accessToken,
      "/api/v1/executions",
      {
        method: "POST",
        body: JSON.stringify({
          kind: "repo_exec",
          ...data,
        }),
      },
      context.verificationRunID,
    );
  });

export const getExecution = createServerFn({ method: "GET" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .inputValidator((data: { executionId: string }) => data)
  .handler(async ({ context, data }) => {
    return sandboxRentalServiceRequest<Execution>(
      context.auth.accessToken,
      `/api/v1/executions/${data.executionId}`,
      undefined,
      context.verificationRunID,
    );
  });

export const importRepo = createServerFn({ method: "POST" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .inputValidator((data: ImportRepoRequest) => data)
  .handler(async ({ context, data }) => {
    return sandboxRentalServiceRequest<Repo>(
      context.auth.accessToken,
      "/api/v1/repos",
      {
        method: "POST",
        body: JSON.stringify(data),
      },
      context.verificationRunID,
    );
  });

export const getRepos = createServerFn({ method: "GET" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return sandboxRentalServiceRequest<Repo[]>(
      context.auth.accessToken,
      "/api/v1/repos",
      undefined,
      context.verificationRunID,
    );
  });

export const getRepo = createServerFn({ method: "GET" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .inputValidator((data: { repoId: string }) => data)
  .handler(async ({ context, data }) => {
    return sandboxRentalServiceRequest<Repo>(
      context.auth.accessToken,
      `/api/v1/repos/${data.repoId}`,
      undefined,
      context.verificationRunID,
    );
  });

export const rescanRepo = createServerFn({ method: "POST" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .inputValidator((data: { repoId: string }) => data)
  .handler(async ({ context, data }) => {
    return sandboxRentalServiceRequest<Repo>(
      context.auth.accessToken,
      `/api/v1/repos/${data.repoId}/rescan`,
      {
        method: "POST",
      },
      context.verificationRunID,
    );
  });

export const getRepoGenerations = createServerFn({ method: "GET" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .inputValidator((data: { repoId: string }) => data)
  .handler(async ({ context, data }) => {
    return sandboxRentalServiceRequest<GoldenGeneration[]>(
      context.auth.accessToken,
      `/api/v1/repos/${data.repoId}/generations`,
      undefined,
      context.verificationRunID,
    );
  });

export const refreshRepo = createServerFn({ method: "POST" })
  .middleware([verificationRunMiddleware, rentASandboxAuthMiddleware])
  .inputValidator((data: { repoId: string }) => data)
  .handler(async ({ context, data }) => {
    return sandboxRentalServiceRequest<RepoBootstrapRecord>(
      context.auth.accessToken,
      `/api/v1/repos/${data.repoId}/refresh`,
      {
        method: "POST",
      },
      context.verificationRunID,
    );
  });
