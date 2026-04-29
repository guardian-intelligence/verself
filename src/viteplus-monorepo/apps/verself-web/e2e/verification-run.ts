import fs from "node:fs/promises";
import path from "node:path";
import { env } from "./env";

export interface VerificationRun {
  verification_run_id: string;
  repo_url: string;
  ref: string;
  repo_id: string;
  import_scanned_sha: string;
  rescan_scanned_sha: string;
  submit_requested_at: string;
  execution_id: string;
  attempt_id: string;
  started_balance: number;
  finished_balance: number;
  status: string;
  detail_url: string;
  log_marker: string;
  terminal_observed_at: string;
  error: string;
}

export function createVerificationRun(verificationRunID: string): VerificationRun {
  return {
    verification_run_id: verificationRunID,
    repo_url: env.verificationRepoURL,
    ref: env.verificationRepoRef,
    repo_id: "",
    import_scanned_sha: "",
    rescan_scanned_sha: "",
    submit_requested_at: new Date().toISOString(),
    execution_id: "",
    attempt_id: "",
    started_balance: 0,
    finished_balance: 0,
    status: "unknown",
    detail_url: "",
    log_marker: env.verificationLogMarker,
    terminal_observed_at: "",
    error: "",
  };
}

export async function persistVerificationRun(
  runJSONPath: string,
  run: VerificationRun,
): Promise<void> {
  if (!run.terminal_observed_at) {
    run.terminal_observed_at = new Date().toISOString();
  }

  await fs.mkdir(path.dirname(runJSONPath), { recursive: true });
  await fs.writeFile(runJSONPath, JSON.stringify(run, null, 2));
}
