import { execFile as execFileCallback } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { test as base, type ConsoleMessage, type Page } from "@playwright/test";
import { env } from "./env";

const execFile = promisify(execFileCallback);

const allowedConsolePatterns: RegExp[] = [
  // Permissions/Feature-Policy crossover noise — the edge stack ships both.
  /Feature-Policy header: Some features are specified in both Feature-Policy and Permissions-Policy header/i,
];

type ConsoleEntry = {
  readonly locationUrl: string;
  readonly text: string;
  readonly type: string;
};

type FailedRequest = {
  readonly failure: string;
  readonly resourceType: string;
  readonly url: string;
};

class BrowserMonitor {
  readonly consoleMessages: ConsoleEntry[] = [];
  readonly failedRequests: FailedRequest[] = [];
  readonly pageErrors: string[] = [];

  constructor(page: Page) {
    page.on("console", (message: ConsoleMessage) => {
      const location = message.location();
      this.consoleMessages.push({
        locationUrl: location.url ?? "",
        text: message.text(),
        type: message.type(),
      });
    });
    page.on("pageerror", (error: Error) => {
      this.pageErrors.push(`[${page.url()}] ${error.stack || String(error)}`);
    });
    page.on("requestfailed", (request) => {
      this.failedRequests.push({
        failure: request.failure()?.errorText || "unknown",
        resourceType: request.resourceType(),
        url: request.url(),
      });
    });
  }

  assertHealthy(): void {
    const baseURL = env.baseURL.replace(/\/+$/, "");
    const unexpectedFailures = this.failedRequests.filter((request) => {
      if (!request.url.startsWith(baseURL)) return false;
      if (request.failure === "net::ERR_ABORTED") return false;
      // The OTel SDK's BatchSpanProcessor flushes on visibilitychange:hidden;
      // when a Playwright run ends the navigation, those flushes can race the
      // navigation cancellation and surface as benign aborts.
      if (request.url.includes("/api/otel/")) return false;
      return !["font", "image", "media", "script", "stylesheet"].includes(request.resourceType);
    });

    const unexpectedConsoleMessages = this.consoleMessages.filter((message) => {
      if (message.locationUrl && !message.locationUrl.startsWith(baseURL)) return false;
      if (allowedConsolePatterns.some((pattern) => pattern.test(message.text))) return false;
      return message.type === "error" || message.type === "warning";
    });

    if (
      this.pageErrors.length === 0 &&
      unexpectedFailures.length === 0 &&
      unexpectedConsoleMessages.length === 0
    ) {
      return;
    }

    throw new Error(
      [
        this.pageErrors[0] ?? "",
        unexpectedFailures[0]
          ? `${unexpectedFailures[0].failure} ${unexpectedFailures[0].url}`
          : "",
        unexpectedConsoleMessages[0]
          ? `${unexpectedConsoleMessages[0].type}: ${unexpectedConsoleMessages[0].text}`
          : "",
      ]
        .filter(Boolean)
        .join(" | "),
    );
  }
}

export interface ClickhouseRow {
  readonly [column: string]: string;
}

// Run a ClickHouse query through the same `aspect db ch query` plumbing the
// operator uses. The query MUST end with `FORMAT TabSeparatedWithNames` (or the
// helper appends it). Returns rows as `{column: value}` objects so tests can
// assert on actual span content rather than waiting blindly for materialisation.
export async function clickhouseQuery(query: string): Promise<ClickhouseRow[]> {
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

interface PlatformFixtures {
  monitor: BrowserMonitor;
}

export const test = base.extend<PlatformFixtures>({
  monitor: async ({ page }, use) => {
    const monitor = new BrowserMonitor(page);
    await use(monitor);
    monitor.assertHealthy();
  },
});

export { expect } from "@playwright/test";
export const PLATFORM_BASE_URL = env.baseURL.replace(/\/+$/, "");
