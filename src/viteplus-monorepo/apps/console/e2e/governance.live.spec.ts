import { execFile as execFileCallback } from "node:child_process";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { ensureTestUserExists, expect, shortTimeoutMS, test } from "./harness";

const execFile = promisify(execFileCallback);

test.describe("Console Governance", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("admin creates an organization data export and emits ClickHouse audit evidence", async ({
    app,
  }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      const exportJobsBefore = await readGovernanceExportJobCount();
      const auditEventsBefore = await readGovernanceCreateAuditCount();
      const downloadedExportsBefore = await readGovernanceDownloadedExportJobCount();
      const downloadAuditEventsBefore = await readGovernanceDownloadAuditCount();

      await app.expectSSRHTML("/settings/governance", [
        "Data export",
        "Audit trail",
        "Create data export",
      ]);
      await app.assertStableRoute({
        path: "/settings/governance",
        ready: app.page.getByRole("heading", { name: "Data export" }),
        expectedText: ["Data export", "Audit trail", "Create data export"],
      });

      await app.page.getByTestId("create-data-export").click();

      let latestExportID = "";
      await app.waitForCondition("governance export job row", shortTimeoutMS, async () => {
        const exportJobsAfter = await readGovernanceExportJobCount();
        if (exportJobsAfter <= exportJobsBefore) {
          return false;
        }
        latestExportID = await readLatestCompletedGovernanceExportID();
        return latestExportID || false;
      });
      await app.waitForCondition("governance create audit row", shortTimeoutMS, async () => {
        const auditEventsAfter = await readGovernanceCreateAuditCount();
        return auditEventsAfter > auditEventsBefore ? auditEventsAfter : false;
      });
      await expect(app.page.getByText("high").first()).toBeVisible({ timeout: shortTimeoutMS });

      const downloadButton = app.page.getByTestId(`download-data-export-${latestExportID}`);
      await expect(downloadButton).toBeVisible({ timeout: shortTimeoutMS });
      const downloadPromise = app.page.waitForEvent("download", { timeout: shortTimeoutMS });
      await downloadButton.click();
      const download = await downloadPromise;
      expect(download.suggestedFilename()).toMatch(/^verself-.+\.tar\.gz$/);
      expect(await download.failure()).toBeNull();

      await app.waitForCondition("governance export downloaded_at", shortTimeoutMS, async () => {
        const downloaded = await readGovernanceExportDownloaded(latestExportID);
        if (downloaded) {
          return latestExportID;
        }
        const downloadedExportsAfter = await readGovernanceDownloadedExportJobCount();
        return downloadedExportsAfter > downloadedExportsBefore ? downloadedExportsAfter : false;
      });
      await app.waitForCondition("governance download audit row", shortTimeoutMS, async () => {
        const downloadAuditEventsAfter = await readGovernanceDownloadAuditCount();
        return downloadAuditEventsAfter > downloadAuditEventsBefore
          ? downloadAuditEventsAfter
          : false;
      });

      const createTraceID = await readLatestGovernanceAuditTraceID("create-data-export");
      await app.waitForCondition("governance create trace sequence", shortTimeoutMS, async () => {
        const spans = await readTraceSpans(createTraceID);
        return hasSpanSubsequence(spans, [
          "governance-service",
          "governance.export.create",
          "governance.export.build",
          "governance.audit.record",
        ])
          ? spans.join(" > ")
          : false;
      });

      const downloadTraceID = await readLatestGovernanceAuditTraceID("download-data-export");
      await app.waitForCondition("governance download trace sequence", shortTimeoutMS, async () => {
        const spans = await readTraceSpans(downloadTraceID);
        return hasSpanSubsequence(spans, [
          "governance-service",
          "governance.export.download",
          "governance.audit.record",
        ])
          ? spans.join(" > ")
          : false;
      });

      run.detail_url = "/settings/governance";
      run.status = "succeeded";
      run.terminal_observed_at = new Date().toISOString();
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      await app.persistRun(run);
    }
  });
});

async function readGovernanceExportJobCount(): Promise<number> {
  const sql = `
    SELECT count(*)::text
    FROM governance_export_jobs
    WHERE state = 'completed'
      AND created_at >= now() - interval '15 minutes';
  `;
  const stdout = await platformScript("pg.sh", [
    "governance_service",
    "--no-align",
    "--tuples-only",
    "--quiet",
    "--query",
    sql,
  ]);
  return parseCount(stdout, "governance export job count");
}

async function readLatestCompletedGovernanceExportID(): Promise<string> {
  const sql = `
    SELECT export_id::text
    FROM governance_export_jobs
    WHERE state = 'completed'
      AND created_at >= now() - interval '15 minutes'
    ORDER BY created_at DESC
    LIMIT 1;
  `;
  const exportID = (
    await platformScript("pg.sh", [
      "governance_service",
      "--no-align",
      "--tuples-only",
      "--quiet",
      "--query",
      sql,
    ])
  ).trim();
  if (!/^[0-9a-f-]{36}$/.test(exportID)) {
    throw new Error(`latest governance export id was invalid: ${exportID}`);
  }
  return exportID;
}

async function readGovernanceCreateAuditCount(): Promise<number> {
  const sql = `
    SELECT count()
    FROM audit_events
    WHERE service_name = 'governance-service'
      AND operation_id = 'create-data-export'
      AND audit_event = 'governance.data_export.create'
      AND source_product_area = 'Governance'
      AND operation_type = 'export'
      AND event_category = 'export'
      AND risk_level = 'high'
      AND actor_type IN ('user', 'api_credential')
      AND length(actor_id) > 0
      AND target_kind = 'data_export'
      AND length(target_id) = 36
      AND decision = 'allow'
      AND result = 'allowed'
      AND recorded_at >= now() - INTERVAL 15 MINUTE
      AND length(content_sha256) = 64
      AND length(client_ip_hash) = 64
      AND length(hmac_key_id) > 0
      AND length(row_hmac) = 64;
  `;
  const stdout = await platformScript("clickhouse.sh", [
    "--database",
    "verself",
    "--format",
    "TabSeparatedRaw",
    "--query",
    sql,
  ]);
  return parseCount(stdout, "governance create audit count");
}

async function readGovernanceDownloadedExportJobCount(): Promise<number> {
  const sql = `
    SELECT count(*)::text
    FROM governance_export_jobs
    WHERE state = 'completed'
      AND downloaded_at IS NOT NULL
      AND downloaded_at >= now() - interval '15 minutes';
  `;
  const stdout = await platformScript("pg.sh", [
    "governance_service",
    "--no-align",
    "--tuples-only",
    "--quiet",
    "--query",
    sql,
  ]);
  return parseCount(stdout, "governance downloaded export job count");
}

async function readGovernanceExportDownloaded(exportID: string): Promise<boolean> {
  if (!/^[0-9a-f-]{36}$/.test(exportID)) {
    throw new Error(`governance export id was invalid: ${exportID}`);
  }
  const sql = `
    SELECT count(*)::text
    FROM governance_export_jobs
    WHERE export_id = '${exportID}'::uuid
      AND state = 'completed'
      AND downloaded_at IS NOT NULL;
  `;
  const stdout = await platformScript("pg.sh", [
    "governance_service",
    "--no-align",
    "--tuples-only",
    "--quiet",
    "--query",
    sql,
  ]);
  return parseCount(stdout, "governance downloaded export id count") > 0;
}

async function readGovernanceDownloadAuditCount(): Promise<number> {
  const sql = `
    SELECT count()
    FROM audit_events
    WHERE service_name = 'governance-service'
      AND operation_id = 'download-data-export'
      AND audit_event = 'governance.data_export.download'
      AND source_product_area = 'Governance'
      AND operation_type = 'export'
      AND event_category = 'export'
      AND risk_level = 'high'
      AND actor_type IN ('user', 'api_credential')
      AND length(actor_id) > 0
      AND target_kind = 'data_export'
      AND length(target_id) = 36
      AND decision = 'allow'
      AND result = 'allowed'
      AND recorded_at >= now() - INTERVAL 15 MINUTE
      AND length(content_sha256) = 64
      AND length(client_ip_hash) = 64
      AND length(hmac_key_id) > 0
      AND length(row_hmac) = 64;
  `;
  const stdout = await platformScript("clickhouse.sh", [
    "--database",
    "verself",
    "--format",
    "TabSeparatedRaw",
    "--query",
    sql,
  ]);
  return parseCount(stdout, "governance download audit count");
}

async function readLatestGovernanceAuditTraceID(
  operationID: "create-data-export" | "download-data-export",
): Promise<string> {
  const sql = `
    SELECT trace_id
    FROM audit_events
    WHERE service_name = 'governance-service'
      AND operation_id = '${operationID}'
      AND result = 'allowed'
      AND recorded_at >= now() - INTERVAL 15 MINUTE
      AND length(trace_id) = 32
    ORDER BY recorded_at DESC
    LIMIT 1;
  `;
  const traceID = (
    await platformScript("clickhouse.sh", [
      "--database",
      "verself",
      "--format",
      "TabSeparatedRaw",
      "--query",
      sql,
    ])
  ).trim();
  if (!/^[0-9a-f]{32}$/.test(traceID)) {
    throw new Error(`governance ${operationID} trace id was invalid: ${traceID}`);
  }
  return traceID;
}

async function readTraceSpans(traceID: string): Promise<string[]> {
  if (!/^[0-9a-f]{32}$/.test(traceID)) {
    throw new Error(`trace id was invalid: ${traceID}`);
  }
  const sql = `
    SELECT SpanName
    FROM otel_traces
    WHERE TraceId = {trace_id:String}
    ORDER BY Timestamp ASC;
  `;
  const stdout = await platformScript("clickhouse.sh", [
    "--database",
    "default",
    "--format",
    "TabSeparatedRaw",
    "--param_trace_id",
    traceID,
    "--query",
    sql,
  ]);
  return stdout.trim().split(/\r?\n/).filter(Boolean);
}

function hasSpanSubsequence(spans: string[], expected: string[]): boolean {
  let index = 0;
  for (const span of spans) {
    if (span === expected[index]) {
      index += 1;
    }
    if (index === expected.length) {
      return true;
    }
  }
  return false;
}

async function platformScript(scriptName: string, args: string[]): Promise<string> {
  const platformDir = fileURLToPath(new URL("../../../../platform/", import.meta.url));
  const scriptPath = fileURLToPath(
    new URL(`scripts/${scriptName}`, new URL("../../../../platform/", import.meta.url)),
  );
  const { stdout } = await execFile(scriptPath, args, { cwd: platformDir, env: process.env });
  return stdout;
}

function parseCount(stdout: string, label: string): number {
  const value = Number.parseInt(stdout.trim(), 10);
  if (!Number.isFinite(value)) {
    throw new Error(`${label} was not numeric: ${stdout}`);
  }
  return value;
}
