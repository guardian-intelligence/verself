import { createHash } from "node:crypto";
import { access, mkdir, readFile, stat } from "node:fs/promises";
import { constants } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { chromium, type BrowserContext, type Page } from "playwright-core";

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
  readonly journey: string;
  readonly site: string;
  readonly verselfBaseUrl: string;
  readonly guardianBaseUrl: string;
  readonly component: string;
  readonly jobId: string;
  readonly deployRunKey: string;
  readonly artifactsDir: string;
};

type ParsedArgs = {
  readonly browserExecutable: string | undefined;
  readonly journey: string | undefined;
  readonly site: string | undefined;
  readonly verselfBaseUrl: string | undefined;
  readonly guardianBaseUrl: string | undefined;
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
    browserExecutable: parsed["browser-executable"],
    component: parsed.component,
    deployRunKey: parsed["deploy-run-key"],
    guardianBaseUrl: parsed["guardian-base-url"],
    jobId: parsed["job-id"],
    journey: parsed.journey,
    site: parsed.site,
    verselfBaseUrl: parsed["verself-base-url"],
  };
}

function baseUrlFor(site: string, override: string | undefined, prodURL: string): string {
  const raw = override ?? (site === "prod" ? prodURL : "");
  if (!raw) {
    throw new Error(`site ${site} requires an explicit base URL`);
  }
  return raw.replace(/\/+$/, "");
}

function resolvePath(input: string): string {
  return path.isAbsolute(input) ? input : path.resolve(input);
}

function defaultArtifactsDir(deployRunKey: string): string {
  const outputDir = process.env.TEST_UNDECLARED_OUTPUTS_DIR;
  if (outputDir) return outputDir;
  return path.join(tmpdir(), `guardian-oidc-canary-${deployRunKey}`);
}

function loadOptions(): Options {
  const args = parseArgs(process.argv.slice(2));
  if (!args.browserExecutable) throw new Error("--browser-executable is required");
  if (!args.journey) throw new Error("--journey is required");
  const deployRunKey =
    args.deployRunKey ?? process.env.VERSELF_DEPLOY_RUN_KEY ?? `manual-${process.pid}`;
  const site = args.site ?? process.env.VERSELF_SITE ?? "prod";
  return {
    artifactsDir: resolvePath(args.artifactsDir ?? defaultArtifactsDir(deployRunKey)),
    browserExecutable: resolvePath(args.browserExecutable),
    component: args.component ?? process.env.VERSELF_COMPONENT ?? "verself_web",
    deployRunKey,
    guardianBaseUrl: baseUrlFor(
      site,
      args.guardianBaseUrl ?? process.env.GUARDIAN_CANARY_BASE_URL,
      "https://guardianintelligence.org",
    ),
    jobId: args.jobId ?? process.env.VERSELF_NOMAD_JOB_ID ?? "verself-web",
    journey: resolvePath(args.journey),
    site,
    verselfBaseUrl: baseUrlFor(
      site,
      args.verselfBaseUrl ?? process.env.VERSELF_CANARY_BASE_URL,
      "https://verself.sh",
    ),
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
    steps.push(stepDetail ? { ...step, detail: stepDetail } : step);
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

async function loadJourneyContract(file: string): Promise<{ sha: string; scenarios: number }> {
  const source = await readFile(file, "utf8");
  const requiredPhrases = [
    "Feature: Continue with Guardian",
    "Scenario: Admin signs into Verself",
    "Scenario: User continues with Guardian",
    "Scenario: Unverified redirect URI is rejected",
  ];
  for (const phrase of requiredPhrases) {
    if (!source.includes(phrase)) {
      throw new Error(`journey contract is missing ${JSON.stringify(phrase)}`);
    }
  }
  return {
    scenarios: source.split("\n").filter((line) => line.trimStart().startsWith("Scenario:")).length,
    sha: sha256(source),
  };
}

function watchPageErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("pageerror", (error) => {
    errors.push(error.message);
  });
  return errors;
}

function assertNoPageErrors(errors: readonly string[]): void {
  if (errors.length > 0) {
    throw new Error(`browser page errors: ${errors.join("; ")}`);
  }
}

async function runBrowserPreflight(options: Options): Promise<Record<string, string | number>> {
  const browser = await chromium.launch({
    args: [
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
      viewport: { height: 720, width: 390 },
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

    await page.goto(`${options.verselfBaseUrl}/.well-known/openid-configuration`, {
      waitUntil: "domcontentloaded",
    });
    const discovery = JSON.parse((await page.locator("body").textContent()) ?? "{}") as {
      readonly issuer?: string;
      readonly authorization_endpoint?: string;
      readonly code_challenge_methods_supported?: readonly string[];
    };
    if (discovery.issuer !== options.verselfBaseUrl) {
      throw new Error(`issuer = ${JSON.stringify(discovery.issuer)}`);
    }
    if (!discovery.authorization_endpoint?.startsWith(`${options.verselfBaseUrl}/oauth/v2/`)) {
      throw new Error(
        `authorization endpoint = ${JSON.stringify(discovery.authorization_endpoint)}`,
      );
    }
    if (!discovery.code_challenge_methods_supported?.includes("S256")) {
      throw new Error("issuer discovery does not advertise S256 PKCE");
    }

    await page.goto(`${options.guardianBaseUrl}/_canary/guardian-oidc`, {
      waitUntil: "domcontentloaded",
    });
    await page.getByRole("button", { name: "Continue with Guardian" }).waitFor();
    assertNoPageErrors(pageErrors);
    return {
      final_url: page.url(),
      issuer: discovery.issuer,
      viewport_height: 720,
      viewport_width: 390,
    };
  } finally {
    if (context) {
      await context.tracing.stop({
        path: path.join(options.artifactsDir, "guardian-oidc-trace.zip"),
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
      return {
        browser: path.basename(options.browserExecutable),
        journey: path.basename(options.journey),
      };
    });
    await runStep(
      steps,
      "journey_contract",
      () => loadJourneyContract(options.journey),
      (result) => ({
        scenarios: result.scenarios,
        source_sha256: result.sha,
      }),
    );
    await runStep(
      steps,
      "guardian_oidc_browser_preflight",
      () => runBrowserPreflight(options),
      (result) => result,
    );
  } catch (error) {
    status = "failed";
    failure = errorMessage(error);
  }

  const result = {
    artifacts_dir: options.artifactsDir,
    component: options.component,
    deploy_run_key: options.deployRunKey,
    duration_ms: Math.round(performance.now() - started),
    failure,
    guardian_base_url: options.guardianBaseUrl,
    job_id: options.jobId,
    schema_version: "verself.guardian_oidc_browser_canary.v1",
    site: options.site,
    status,
    steps,
    verself_base_url: options.verselfBaseUrl,
  };

  process.stdout.write(`${JSON.stringify(result)}\n`);
  if (status === "failed") {
    process.exitCode = 1;
  }
}

await main();
