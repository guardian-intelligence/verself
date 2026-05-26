import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { access, mkdir, stat } from "node:fs/promises";
import { constants } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import { chromium, type BrowserContext, type Page } from "playwright-core";

const execFileAsync = promisify(execFile);

const navigationTimeoutMs = 5_000;
const assertionTimeoutMs = 3_000;

type CanaryStatus = "passed" | "failed";

type StepRecord = {
  readonly name: string;
  readonly status: CanaryStatus;
  readonly duration_ms: number;
  readonly detail?: Record<string, string | number | boolean>;
  readonly error?: string;
};

type Options = {
  readonly browserExecutable: string;
  readonly cli: string;
  readonly site: string;
  readonly baseUrl: string;
  readonly component: string;
  readonly jobId: string;
  readonly deployRunKey: string;
  readonly artifactsDir: string;
};

type ParsedArgs = {
  readonly browserExecutable: string | undefined;
  readonly cli: string | undefined;
  readonly site: string | undefined;
  readonly baseUrl: string | undefined;
  readonly component: string | undefined;
  readonly jobId: string | undefined;
  readonly deployRunKey: string | undefined;
  readonly artifactsDir: string | undefined;
};

function parseArgs(argv: readonly string[]): ParsedArgs {
  const parsed: Record<string, string> = {};
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i]!;
    if (!arg.startsWith("--")) {
      throw new Error(`unexpected positional argument: ${arg}`);
    }
    const eq = arg.indexOf("=");
    if (eq > 0) {
      parsed[arg.slice(2, eq)] = arg.slice(eq + 1);
      continue;
    }
    const next = argv[i + 1];
    if (!next || next.startsWith("--")) {
      throw new Error(`missing value for ${arg}`);
    }
    parsed[arg.slice(2)] = next;
    i += 1;
  }
  return {
    artifactsDir: parsed["artifacts-dir"],
    baseUrl: parsed["base-url"],
    browserExecutable: parsed["browser-executable"],
    cli: parsed.cli,
    component: parsed.component,
    deployRunKey: parsed["deploy-run-key"],
    jobId: parsed["job-id"],
    site: parsed.site,
  };
}

function baseUrlFor(site: string, override?: string): string {
  const raw = override ?? (site === "prod" ? "https://verself.sh" : "");
  if (!raw) {
    throw new Error(`site ${site} requires --base-url`);
  }
  return raw.replace(/\/+$/, "");
}

function resolvePath(input: string): string {
  return path.isAbsolute(input) ? input : path.resolve(input);
}

function defaultArtifactsDir(deployRunKey: string): string {
  const outputDir = process.env.TEST_UNDECLARED_OUTPUTS_DIR;
  if (outputDir) return outputDir;
  return path.join(tmpdir(), `verself-web-e2e-${deployRunKey}`);
}

function loadOptions(): Options {
  const args = parseArgs(process.argv.slice(2));
  if (!args.browserExecutable) throw new Error("--browser-executable is required");
  if (!args.cli) throw new Error("--cli is required");
  const deployRunKey =
    args.deployRunKey ?? process.env.VERSELF_DEPLOY_RUN_KEY ?? `manual-${process.pid}`;
  const site = args.site ?? process.env.VERSELF_SITE ?? "prod";
  return {
    artifactsDir: resolvePath(args.artifactsDir ?? defaultArtifactsDir(deployRunKey)),
    baseUrl: baseUrlFor(site, args.baseUrl),
    browserExecutable: resolvePath(args.browserExecutable),
    cli: resolvePath(args.cli),
    component: args.component ?? process.env.VERSELF_COMPONENT ?? "verself_web",
    deployRunKey,
    jobId: args.jobId ?? process.env.VERSELF_NOMAD_JOB_ID ?? "verself-web",
    site,
  };
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return String(error);
}

function sha256(input: string): string {
  return createHash("sha256").update(input).digest("hex");
}

async function assertExecutable(file: string): Promise<void> {
  await stat(file);
  await access(file, constants.X_OK);
}

async function runStep<T>(
  steps: StepRecord[],
  name: string,
  fn: () => Promise<T>,
  detail?: (value: T) => StepRecord["detail"],
): Promise<T> {
  const start = performance.now();
  try {
    const value = await fn();
    const step: StepRecord = {
      duration_ms: Math.round(performance.now() - start),
      name,
      status: "passed",
    };
    const stepDetail = detail?.(value);
    if (stepDetail) {
      steps.push({ ...step, detail: stepDetail });
    } else {
      steps.push(step);
    }
    return value;
  } catch (error) {
    steps.push({
      duration_ms: Math.round(performance.now() - start),
      error: errorMessage(error),
      name,
      status: "failed",
    });
    throw error;
  }
}

async function runCliSmoke(cli: string): Promise<{ stdoutHash: string; usageLines: number }> {
  const { stdout } = await execFileAsync(cli, ["--help"], {
    encoding: "utf8",
    timeout: 5_000,
  });
  if (!stdout.includes("signup EMAIL")) {
    throw new Error("CLI help is missing signup usage");
  }
  if (!stdout.includes("whoami [--json]")) {
    throw new Error("CLI help is missing whoami usage");
  }
  return {
    stdoutHash: sha256(stdout),
    usageLines: stdout.trimEnd().split("\n").length,
  };
}

function watchPageErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("pageerror", (error) => {
    errors.push(error.message);
  });
  return errors;
}

async function assertNoPageErrors(errors: readonly string[]): Promise<void> {
  if (errors.length > 0) {
    throw new Error(`browser page errors: ${errors.join("; ")}`);
  }
}

async function runBrowserSmoke(options: Options): Promise<Record<string, string | number>> {
  const browser = await chromium.launch({
    args: [
      // Firecracker canaries run without a user namespace sandbox and may have small /dev/shm.
      "--no-sandbox",
      "--disable-dev-shm-usage",
      "--disable-background-networking",
      "--disable-default-apps",
      "--disable-extensions",
      "--disable-sync",
      "--metrics-recording-only",
      "--no-first-run",
    ],
    chromiumSandbox: false,
    executablePath: options.browserExecutable,
    headless: true,
    timeout: 10_000,
  });
  let context: BrowserContext | undefined;
  try {
    context = await browser.newContext({
      baseURL: options.baseUrl,
      viewport: { height: 667, width: 375 },
    });
    await context.tracing.start({
      screenshots: true,
      snapshots: true,
      sources: false,
    });
    const page = await context.newPage();
    page.setDefaultNavigationTimeout(navigationTimeoutMs);
    page.setDefaultTimeout(assertionTimeoutMs);
    const pageErrors = watchPageErrors(page);

    await page.goto("/readyz", { waitUntil: "domcontentloaded" });
    const readyBody = (await page.locator("body").textContent())?.trim();
    if (readyBody !== "ok") {
      throw new Error(`/readyz returned ${JSON.stringify(readyBody)}`);
    }

    await page.goto("/", { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: "Verself" }).waitFor();
    await page.getByRole("link", { name: "Get Verself" }).waitFor();

    await page.goto("/login", { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: "Sign in to Console" }).waitFor();
    await page.getByRole("button", { name: /Continue to sign in/i }).waitFor();
    await assertNoPageErrors(pageErrors);

    return {
      final_url: page.url(),
      viewport_height: 667,
      viewport_width: 375,
    };
  } finally {
    if (context) {
      await context.tracing.stop({
        path: path.join(options.artifactsDir, "trace.zip"),
      });
      await context.close();
    }
    await browser.close();
  }
}

async function main(): Promise<void> {
  const started = performance.now();
  const steps: StepRecord[] = [];
  const options = loadOptions();
  await mkdir(options.artifactsDir, { recursive: true });

  let status: CanaryStatus = "passed";
  let failure = "";
  try {
    await runStep(steps, "inputs", async () => {
      await assertExecutable(options.browserExecutable);
      await assertExecutable(options.cli);
      return {
        browser: path.basename(options.browserExecutable),
        cli: path.basename(options.cli),
      };
    });
    await runStep(
      steps,
      "cli_help",
      () => runCliSmoke(options.cli),
      (result) => ({
        stdout_sha256: result.stdoutHash,
        usage_lines: result.usageLines,
      }),
    );
    await runStep(
      steps,
      "browser_smoke",
      () => runBrowserSmoke(options),
      (result) => result,
    );
  } catch (error) {
    status = "failed";
    failure = errorMessage(error);
  }

  const result = {
    artifacts_dir: options.artifactsDir,
    base_url: options.baseUrl,
    component: options.component,
    deploy_run_key: options.deployRunKey,
    duration_ms: Math.round(performance.now() - started),
    failure,
    job_id: options.jobId,
    schema_version: "verself.browser_canary.v1",
    site: options.site,
    status,
    steps,
  };

  process.stdout.write(`${JSON.stringify(result)}\n`);
  if (status === "failed") {
    process.exitCode = 1;
  }
}

await main();
