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

async function runAuthProductScenario(page: Page): Promise<number> {
  let visited = 0;

  await givenTheVisitorCanOpenTheAuthShell(page);
  visited += 1;
  await thenTheLoginFormIsPasswordManagerReady(page);
  await thenTheLoginFormUsesBlurValidation(page);

  await givenTheVisitorCanOpenTheAuthShell(page);
  await whenTheVisitorSignsInWithIncorrectCredentials(page);
  await thenIncorrectEmailOrPasswordDisplaysAToast(page);

  await whenZitadelRoutesAnAuthRequestToVerself(page);
  visited += 1;
  await thenTheAuthRequestStaysOnTheVerselfLoginForm(page);

  await whenTheVisitorStartsPasswordRecovery(page);
  visited += 1;
  await thenTheForgotPasswordFlowIsEmailOnly(page);
  await thenTheForgotPasswordFormUsesBlurValidation(page);

  await whenTheVisitorStartsSignup(page);
  visited += 1;
  await thenTheSignupFlowCollectsOrganizationIdentity(page);
  await thenTheSignupFormUsesSchemaValidation(page);

  await whenTheVisitorOpensAResetLinkWithoutToken(page);
  visited += 1;
  await thenTheResetFlowOffersFreshRecovery(page);

  await whenTheVisitorOpensAResetLinkWithToken(page);
  visited += 1;
  await thenTheResetFormValidatesPasswordConfirmation(page);

  await whenTheVisitorOpensADeviceCodeLink(page, "VERS-ELF1");
  visited += 1;
  await thenTheDeviceCodeIsPrefilled(page, "VERS-ELF1");
  await thenTheDeviceCodeFormUsesBlurValidation(page);

  return visited;
}

async function givenTheVisitorCanOpenTheAuthShell(page: Page): Promise<void> {
  await page.goto("/login", { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: "Sign in" }).waitFor();
}

async function thenTheLoginFormIsPasswordManagerReady(page: Page): Promise<void> {
  await page.getByLabel("Email").waitFor();
  await page.getByLabel("Password", { exact: true }).waitFor();
  await page.getByRole("button", { name: "Sign in" }).waitFor();
  await page.getByRole("link", { name: "Forgot password?" }).waitFor();
  await waitForEnabledButton(page, "Sign in");
}

async function thenTheLoginFormUsesBlurValidation(page: Page): Promise<void> {
  const email = page.getByLabel("Email");
  await email.fill("not-an-email");
  await email.blur();
  await page.getByText("Enter a valid email address.").waitFor();
  const password = page.getByLabel("Password", { exact: true });
  await password.focus();
  await password.blur();
  await page.getByText("Password is required.").waitFor();
  if (!(await page.getByRole("button", { name: "Sign in" }).isDisabled())) {
    throw new Error("invalid login form left Sign in enabled");
  }
}

async function whenTheVisitorSignsInWithIncorrectCredentials(page: Page): Promise<void> {
  const email = page.getByLabel("Email");
  await email.fill(`wrong-${Date.now()}@example.test`);
  await email.blur();
  const password = page.getByLabel("Password", { exact: true });
  await password.fill("not the right password");
  await password.blur();
  await waitForEnabledButton(page, "Sign in");
  await page.getByRole("button", { name: "Sign in" }).click();
}

async function thenIncorrectEmailOrPasswordDisplaysAToast(page: Page): Promise<void> {
  await page.getByText("Email or password is incorrect.").waitFor();
  const path = new URL(page.url()).pathname;
  if (path !== "/login") {
    throw new Error(`incorrect password changed route to ${path}`);
  }
}

async function whenZitadelRoutesAnAuthRequestToVerself(page: Page): Promise<void> {
  await page.goto("/login?authRequest=V2_123&login_hint=founder%40example.test", {
    waitUntil: "domcontentloaded",
  });
}

async function thenTheAuthRequestStaysOnTheVerselfLoginForm(page: Page): Promise<void> {
  await page.getByRole("heading", { name: "Sign in" }).waitFor();
  const email = page.getByLabel("Email");
  await email.waitFor();
  const value = await email.inputValue();
  if (value !== "founder@example.test") {
    throw new Error(`auth request login hint = ${JSON.stringify(value)}`);
  }
  if (page.url().includes("/ui/login")) {
    throw new Error("auth request fell through to Zitadel UI");
  }
}

async function whenTheVisitorStartsPasswordRecovery(page: Page): Promise<void> {
  await page.getByRole("link", { name: "Forgot password?" }).click();
  await page.waitForURL("**/forgot-password");
}

async function thenTheForgotPasswordFlowIsEmailOnly(page: Page): Promise<void> {
  await page.getByRole("heading", { name: "Reset password" }).waitFor();
  await page.getByLabel("Email").waitFor();
  await page.getByRole("button", { name: "Send reset email" }).waitFor();
  await waitForEnabledButton(page, "Send reset email");
}

async function thenTheForgotPasswordFormUsesBlurValidation(page: Page): Promise<void> {
  const email = page.getByLabel("Email");
  await email.fill("invalid");
  await email.blur();
  await page.getByText("Enter a valid email address.").waitFor();
  if (!(await page.getByRole("button", { name: "Send reset email" }).isDisabled())) {
    throw new Error("invalid forgot-password form left Send reset email enabled");
  }
}

async function whenTheVisitorStartsSignup(page: Page): Promise<void> {
  await page.goto("/signup", { waitUntil: "domcontentloaded" });
}

async function thenTheSignupFlowCollectsOrganizationIdentity(page: Page): Promise<void> {
  await page.getByRole("heading", { name: "Create account" }).waitFor();
  await page.getByLabel("Email").waitFor();
  await page.getByLabel("Organization name").waitFor();
  await page.getByText("verself.sh/").waitFor();
  await page.getByRole("button", { name: "Create account" }).waitFor();
  await waitForEnabledButton(page, "Create account");
}

async function thenTheSignupFormUsesSchemaValidation(page: Page): Promise<void> {
  const email = page.getByLabel("Email");
  await email.fill("not-an-email");
  await email.blur();
  await page.getByText("Enter a valid email address.").waitFor();
  const organization = page.getByLabel("Organization name");
  await organization.focus();
  await organization.blur();
  await page.getByText("Organization name is required.").waitFor();
  if (!(await page.getByRole("button", { name: "Create account" }).isDisabled())) {
    throw new Error("invalid signup form left Create account enabled");
  }
}

async function whenTheVisitorOpensAResetLinkWithoutToken(page: Page): Promise<void> {
  await page.goto("/reset-password", { waitUntil: "domcontentloaded" });
}

async function whenTheVisitorOpensAResetLinkWithToken(page: Page): Promise<void> {
  await page.goto("/reset-password?user_id=user_test&verification_code=code_test", {
    waitUntil: "domcontentloaded",
  });
}

async function thenTheResetFlowOffersFreshRecovery(page: Page): Promise<void> {
  await page.getByRole("heading", { name: "Set password" }).waitFor();
  await page.getByRole("link", { name: "Request a new reset email" }).waitFor();
}

async function thenTheResetFormValidatesPasswordConfirmation(page: Page): Promise<void> {
  await page.getByRole("heading", { name: "Set password" }).waitFor();
  await waitForEnabledButton(page, "Save password");
  await page.getByLabel("New password").fill("correct horse battery staple");
  await page.getByLabel("Confirm password").fill("wrong passphrase value");
  await page.getByLabel("Confirm password").blur();
  await page.getByText("Passwords do not match.").waitFor();
  if (!(await page.getByRole("button", { name: "Save password" }).isDisabled())) {
    throw new Error("invalid reset form left Save password enabled");
  }
}

async function whenTheVisitorOpensADeviceCodeLink(page: Page, userCode: string): Promise<void> {
  await page.goto(`/device?user_code=${encodeURIComponent(userCode)}`, {
    waitUntil: "domcontentloaded",
  });
}

async function thenTheDeviceCodeIsPrefilled(page: Page, userCode: string): Promise<void> {
  await page.getByRole("heading", { name: "Approve device" }).waitFor();
  const input = page.getByLabel("Device code");
  await input.waitFor();
  const value = await input.inputValue();
  if (value !== userCode) {
    throw new Error(
      `device code prefill = ${JSON.stringify(value)}, want ${JSON.stringify(userCode)}`,
    );
  }
  await page.getByRole("button", { name: "Continue" }).waitFor();
  await waitForEnabledButton(page, "Continue");
}

async function thenTheDeviceCodeFormUsesBlurValidation(page: Page): Promise<void> {
  const input = page.getByLabel("Device code");
  await input.fill("x");
  await input.blur();
  await page.getByText("Enter the device code from your terminal.").waitFor();
  if (!(await page.getByRole("button", { name: "Continue" }).isDisabled())) {
    throw new Error("invalid device form left Continue enabled");
  }
}

async function waitForEnabledButton(page: Page, name: string): Promise<void> {
  const button = page.getByRole("button", { name });
  await button.waitFor();
  const started = performance.now();
  while (await button.isDisabled()) {
    if (performance.now() - started > assertionTimeoutMs) {
      throw new Error(`${name} button did not hydrate into an enabled state`);
    }
    await page.waitForTimeout(50);
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

    const authPages = await runAuthProductScenario(page);
    await assertNoPageErrors(pageErrors);

    return {
      auth_pages: authPages,
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
