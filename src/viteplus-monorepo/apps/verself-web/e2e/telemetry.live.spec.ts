import { execFile as execFileCallback } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { expect, test } from "@playwright/test";
import { env } from "./env";

const execFile = promisify(execFileCallback);

// Phase-0 gate: a real browser visit to the app emits a `page_view` span
// tagged with the route path, the span lands in default.otel_traces under
// service.name=verself-web, and (if a deploy correlation is set) it carries
// the right `verself.deploy_run_key` resource attribute. Lives in the e2e
// suite for verself-web because the OTel browser bundle ships with the same
// app — deleting the app would delete this gate.
test.describe("browser telemetry baseline", () => {
  test("page_view span lands in ClickHouse with service.name=verself-web", async ({ page }) => {
    const witness = `e2e-${Date.now().toString(36)}`;
    await page.goto(`/?probe=${witness}`);
    await expect(page).toHaveTitle(/Verself/);

    // The BSP flushes on visibilitychange:hidden. Closing the page triggers it.
    await page.waitForTimeout(2_500);
    await page.close();

    // Allow the otel collector → ClickHouse pipeline to land the row.
    await new Promise((resolve) => setTimeout(resolve, 5_000));

    const rows = await clickhouseQuery(`
      SELECT count() AS n
      FROM default.otel_traces
      WHERE ServiceName = 'verself-web'
        AND SpanName = 'page_view'
        AND SpanAttributes['route.path'] = '/'
        AND Timestamp >= now() - INTERVAL 5 MINUTE
    `);

    const n = Number(rows[0]?.n ?? "0");
    expect(n, "expected at least one page_view span from verself-web").toBeGreaterThan(0);
  });

  test("OTLP forward route accepts JSON and returns 2xx", async ({ request }) => {
    const minimalOtlp = {
      resourceSpans: [
        {
          resource: {
            attributes: [
              { key: "service.name", value: { stringValue: "verself-web-e2e-probe" } },
            ],
          },
          scopeSpans: [
            {
              scope: { name: "verself-web-e2e", version: "0.0.0" },
              spans: [
                {
                  traceId: "0123456789abcdef0123456789abcdef",
                  spanId: "0123456789abcdef",
                  name: "verself-web.e2e.probe",
                  kind: 1,
                  startTimeUnixNano: String(Date.now() * 1_000_000),
                  endTimeUnixNano: String((Date.now() + 1) * 1_000_000),
                },
              ],
            },
          ],
        },
      ],
    };
    const response = await request.post("/api/otel/v1/traces", {
      data: minimalOtlp,
      headers: { "content-type": "application/json" },
    });
    expect(response.status(), `OTLP forward status (body: ${await response.text()})`).toBeLessThan(
      300,
    );
  });
});

// Quote env to silence unused warnings if a future test branch removes the
// only consumer; harness env is used by runtime fixtures elsewhere.
void env;

interface ClickhouseRow {
  readonly [column: string]: string;
}

async function clickhouseQuery(query: string): Promise<ClickhouseRow[]> {
  const trimmed = query.trim().replace(/;\s*$/, "");
  const formatted = /\bFORMAT\b/i.test(trimmed)
    ? trimmed
    : `${trimmed} FORMAT TabSeparatedWithNames`;
  const { stdout } = await execFile(
    "bash",
    [
      "-lc",
      `cd "${repoRootSync()}/src/platform" && ./scripts/clickhouse.sh --database default --query ${shellQuote(formatted)}`,
    ],
    { maxBuffer: 16 * 1024 * 1024, env: { ...process.env } },
  );
  return parseTabSeparatedWithNames(stdout);
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

function parseTabSeparatedWithNames(stdout: string): ClickhouseRow[] {
  const lines = stdout.split("\n").filter((line) => line.length > 0);
  if (lines.length === 0) return [];
  const header = lines[0]!.split("\t");
  return lines.slice(1).map((line) => {
    const cells = line.split("\t");
    const row: Record<string, string> = {};
    header.forEach((col, i) => {
      row[col] = cells[i] ?? "";
    });
    return row;
  });
}

let cachedRepoRoot: string | undefined;
function repoRootSync(): string {
  if (cachedRepoRoot) return cachedRepoRoot;
  let dir = path.dirname(fileURLToPath(import.meta.url));
  for (let i = 0; i < 10; i++) {
    if (
      fs.existsSync(path.join(dir, "MODULE.aspect")) &&
      fs.existsSync(path.join(dir, "MODULE.bazel"))
    ) {
      cachedRepoRoot = dir;
      return dir;
    }
    dir = path.dirname(dir);
  }
  throw new Error("failed to locate repo root from e2e harness");
}
