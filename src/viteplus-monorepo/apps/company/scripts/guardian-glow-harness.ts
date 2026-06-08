import { spawn } from "node:child_process";
import {
  closeSync,
  existsSync,
  mkdirSync,
  openSync,
  readFileSync,
  readdirSync,
  statSync,
} from "node:fs";
import { appendFile, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  chromium,
  type Browser,
  type BrowserContext,
  type CDPSession,
  type Page,
} from "playwright-core";

type Command = "start" | "run" | "tail" | "status";

type ParsedCli = {
  readonly command: Command;
  readonly values: ReadonlyMap<string, string>;
};

type Viewport = {
  readonly width: number;
  readonly height: number;
};

type HarnessOptions = {
  readonly command: Command;
  readonly url: string;
  readonly baseUrl: string;
  readonly artifactsRoot: string;
  readonly browserExecutable: string;
  readonly durationMs: number;
  readonly launchTimeoutMs: number;
  readonly navigationTimeoutMs: number;
  readonly preflightTimeoutMs: number;
  readonly runId: string;
  readonly screenshotCount: number;
  readonly selectorTimeoutMs: number;
  readonly viewport: Viewport;
};

type RunPaths = {
  readonly eventsPath: string;
  readonly latestPath: string;
  readonly processLogPath: string;
  readonly runDir: string;
  readonly runJsonPath: string;
  readonly screenshotsDir: string;
  readonly stderrPath: string;
  readonly summaryPath: string;
  readonly tracePath: string;
};

type RunPointer = {
  readonly artifacts_dir: string;
  readonly events_path: string;
  readonly pid: number;
  readonly process_log_path: string;
  readonly run_id: string;
  readonly screenshot_dir: string;
  readonly started_at: string;
  readonly status: "queued" | "running" | "completed" | "failed";
  readonly summary_path: string;
  readonly tail_command: string;
  readonly target_url: string;
  readonly trace_path: string;
};

type DomProbe = {
  readonly body_text_hash: string;
  readonly canvas_count: number;
  readonly device_pixel_ratio: number;
  readonly draws: number | null;
  readonly dpr_text: string;
  readonly fps: number | null;
  readonly frame_ms: number | null;
  readonly pixels_text: string;
  readonly probe_count: number | null;
  readonly probe_ms: number | null;
  readonly probe_rate_text: string;
  readonly probe_res_text: string;
  readonly programs: number | null;
  readonly title: string;
  readonly triangles: number | null;
  readonly url: string;
  readonly viewport: Viewport;
};

type CdpProbe = {
  readonly dom_nodes: number | null;
  readonly js_heap_total_mb: number | null;
  readonly js_heap_used_mb: number | null;
  readonly layout_count: number | null;
  readonly layout_duration_ms: number | null;
  readonly recalc_style_count: number | null;
  readonly script_duration_ms: number | null;
  readonly task_duration_ms: number | null;
};

type Sample = {
  readonly cdp: CdpProbe;
  readonly dom: DomProbe;
  readonly elapsed_ms: number;
  readonly screenshot_path: string;
  readonly second: number;
};

const scriptPath = fileURLToPath(import.meta.url);
const appRoot = path.dirname(path.dirname(scriptPath));
const defaultArtifactsRoot = path.join(appRoot, "artifacts", "guardian-glow-runs");
const defaultUrl = [
  "http://127.0.0.1:4260/",
  "?stage=composite",
  "&threshold=0.38",
  "&knee=0.18",
  "&gradient=0.12",
  "&blur=14.5",
  "&bloom=3",
  "&aberration=7.2",
  "&keyAzimuth=-8",
  "&keyElevation=14",
  "&keyDistance=2.25",
  "&cardSize=1",
  "&rigMotion=0.65",
  "&warm=0.62",
  "&cardIntensity=1.6",
  "&lightIntensity=1.2",
  "&sourceIntensity=2.1",
  "&sourceRadius=1.42",
  "&rayIntensity=0.55",
  "&rayLength=0.52",
  "&probeCadence=6",
  "&metalness=0.62",
  "&roughness=0.32",
  "&clearcoat=0.92",
  "&env=1.75",
  "&exposure=1.08",
  "&profile=no-msaa",
  "&developmentMode=1",
].join("");

async function main(): Promise<void> {
  if (process.argv.includes("--help") || process.argv.includes("-h")) {
    printHelp();
    return;
  }

  const parsed = parseCli(process.argv.slice(2));
  const options = loadOptions(parsed);

  switch (options.command) {
    case "start":
      await startBackgroundRun(options);
      return;
    case "run":
      await runHarness(options);
      return;
    case "tail":
      await tailEvents(options);
      return;
    case "status":
      await printStatus(options);
      return;
  }
}

function parseCli(argv: readonly string[]): ParsedCli {
  const command = parseCommand(argv[0]);
  const values = new Map<string, string>();
  for (let index = 1; index < argv.length; index += 1) {
    const arg = argv[index]!;
    if (arg === "--") {
      continue;
    }
    if (!arg.startsWith("--")) {
      throw new Error(`unexpected positional argument: ${arg}`);
    }
    const eq = arg.indexOf("=");
    if (eq > 0) {
      values.set(arg.slice(2, eq), arg.slice(eq + 1));
      continue;
    }
    const next = argv[index + 1];
    if (!next || next.startsWith("--")) {
      throw new Error(`missing value for ${arg}`);
    }
    values.set(arg.slice(2), next);
    index += 1;
  }
  return { command, values };
}

function parseCommand(value: string | undefined): Command {
  if (value === "start" || value === "run" || value === "tail" || value === "status") {
    return value;
  }
  if (!value) {
    return "start";
  }
  throw new Error(`unknown command: ${value}`);
}

function loadOptions(parsed: ParsedCli): HarnessOptions {
  const url = readString(parsed.values, "url", process.env.GUARDIAN_GLOW_URL ?? defaultUrl);
  const browserExecutable = readString(
    parsed.values,
    "browser-executable",
    process.env.GUARDIAN_GLOW_BROWSER ?? defaultBrowserExecutable(),
  );
  return {
    artifactsRoot: resolvePath(
      readString(
        parsed.values,
        "artifacts-dir",
        process.env.GUARDIAN_GLOW_ARTIFACTS ?? defaultArtifactsRoot,
      ),
    ),
    baseUrl: readString(
      parsed.values,
      "base-url",
      process.env.GUARDIAN_GLOW_BASE_URL ?? originFor(url),
    ),
    browserExecutable: resolveBrowserExecutable(browserExecutable),
    command: parsed.command,
    durationMs: readPositiveInteger(parsed.values, "duration-ms", 5_000),
    launchTimeoutMs: readPositiveInteger(parsed.values, "launch-timeout-ms", 10_000),
    navigationTimeoutMs: readPositiveInteger(parsed.values, "navigation-timeout-ms", 2_000),
    preflightTimeoutMs: readPositiveInteger(parsed.values, "preflight-timeout-ms", 750),
    runId: readString(parsed.values, "run-id", ""),
    screenshotCount: readPositiveInteger(parsed.values, "screenshots", 5),
    selectorTimeoutMs: readPositiveInteger(parsed.values, "selector-timeout-ms", 2_000),
    url,
    viewport: parseViewport(readString(parsed.values, "viewport", "1440x960")),
  };
}

function readString(values: ReadonlyMap<string, string>, key: string, fallback: string): string {
  const value = values.get(key);
  if (value === undefined || value.trim() === "") {
    return fallback;
  }
  return value;
}

function readPositiveInteger(
  values: ReadonlyMap<string, string>,
  key: string,
  fallback: number,
): number {
  const raw = values.get(key);
  if (raw === undefined || raw.trim() === "") {
    return fallback;
  }
  const parsed = Number.parseInt(raw, 10);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new Error(`--${key} must be a positive integer`);
  }
  return parsed;
}

function parseViewport(raw: string): Viewport {
  const match = /^(\d+)x(\d+)$/.exec(raw);
  if (!match) {
    throw new Error("--viewport must look like 1440x960");
  }
  const width = Number.parseInt(match[1]!, 10);
  const height = Number.parseInt(match[2]!, 10);
  if (width < 320 || height < 240) {
    throw new Error("--viewport is too small to produce useful scene evidence");
  }
  return { height, width };
}

function originFor(rawUrl: string): string {
  return new URL(rawUrl).origin;
}

function resolvePath(input: string): string {
  return path.isAbsolute(input) ? input : path.resolve(appRoot, input);
}

function resolveBrowserExecutable(input: string): string {
  return input === "chrome" ? input : resolvePath(input);
}

function defaultBrowserExecutable(): string {
  const candidates = [
    process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH,
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
  ];
  for (const candidate of candidates) {
    if (candidate && existsSync(candidate)) {
      return candidate;
    }
  }
  return "chrome";
}

async function startBackgroundRun(options: HarnessOptions): Promise<void> {
  const runId = options.runId || createRunId();
  const runOptions = { ...options, runId };
  const paths = resolveRunPaths(runOptions);
  mkdirSync(paths.screenshotsDir, { recursive: true });

  await writeRunPointer(runOptions, paths, {
    artifacts_dir: paths.runDir,
    events_path: paths.eventsPath,
    pid: process.pid,
    process_log_path: paths.processLogPath,
    run_id: runId,
    screenshot_dir: paths.screenshotsDir,
    started_at: new Date().toISOString(),
    status: "queued",
    summary_path: paths.summaryPath,
    tail_command: tailCommand(runId),
    target_url: runOptions.url,
    trace_path: paths.tracePath,
  });
  await writeEvent(paths, {
    run_id: runId,
    target_url: runOptions.url,
    type: "run_queued",
  });
  await runPreflight(runOptions, paths, "start_preflight");

  const processFd = openSync(paths.processLogPath, "a");
  const stderrFd = openSync(paths.stderrPath, "a");
  const child = spawn(
    resolveVpBinary(),
    ["exec", "tsx", scriptPath, "run", ...serializeChildOptions(runOptions)],
    {
      cwd: appRoot,
      detached: true,
      stdio: ["ignore", processFd, stderrFd],
    },
  );
  child.unref();
  closeSync(processFd);
  closeSync(stderrFd);

  const pointer: RunPointer = {
    artifacts_dir: paths.runDir,
    events_path: paths.eventsPath,
    pid: child.pid ?? 0,
    process_log_path: paths.processLogPath,
    run_id: runId,
    screenshot_dir: paths.screenshotsDir,
    started_at: new Date().toISOString(),
    status: "running",
    summary_path: paths.summaryPath,
    tail_command: tailCommand(runId),
    target_url: runOptions.url,
    trace_path: paths.tracePath,
  };
  await writeRunPointer(runOptions, paths, pointer);
  await writeEvent(paths, {
    pid: pointer.pid,
    tail_command: pointer.tail_command,
    type: "run_spawned",
  });
  console.log(JSON.stringify(pointer, null, 2));
}

async function runHarness(options: HarnessOptions): Promise<void> {
  const runId = options.runId || createRunId();
  const runOptions = { ...options, runId };
  const paths = resolveRunPaths(runOptions);
  await mkdir(paths.screenshotsDir, { recursive: true });

  const samples: Sample[] = [];
  let browser: Browser | null = null;
  let context: BrowserContext | null = null;
  let traceStarted = false;
  let traceStopped = false;

  try {
    await writeEvent(paths, {
      duration_ms: runOptions.durationMs,
      run_id: runId,
      screenshot_count: runOptions.screenshotCount,
      target_url: runOptions.url,
      type: "run_started",
      viewport: runOptions.viewport,
    });
    await runPreflight(runOptions, paths, "run_preflight");

    const launchOptions =
      runOptions.browserExecutable === "chrome"
        ? {
            args: browserLaunchArgs(),
            channel: "chrome" as const,
            headless: true,
            timeout: runOptions.launchTimeoutMs,
          }
        : {
            args: browserLaunchArgs(),
            executablePath: runOptions.browserExecutable,
            headless: true,
            timeout: runOptions.launchTimeoutMs,
          };
    browser = await chromium.launch(launchOptions);
    await writeEvent(paths, {
      browser_executable: runOptions.browserExecutable,
      type: "browser_launched",
    });

    context = await browser.newContext({
      deviceScaleFactor: 1,
      viewport: runOptions.viewport,
    });
    await context.tracing.start({
      screenshots: true,
      snapshots: true,
      sources: false,
      title: "guardian-glow-harness",
    });
    traceStarted = true;

    const page = await context.newPage();
    watchPage(page, paths);
    const cdp = await context.newCDPSession(page);
    await cdp.send("Performance.enable");

    const navigationStart = performance.now();
    const response = await page.goto(runOptions.url, {
      timeout: runOptions.navigationTimeoutMs,
      waitUntil: "domcontentloaded",
    });
    await writeEvent(paths, {
      duration_ms: Math.round(performance.now() - navigationStart),
      final_url: page.url(),
      status: response?.status() ?? null,
      type: "navigation_complete",
    });

    const captureStart = performance.now();
    const canvasReady = page
      .locator("canvas")
      .first()
      .waitFor({
        state: "visible",
        timeout: Math.min(runOptions.selectorTimeoutMs, runOptions.durationMs),
      })
      .then(async () => {
        await writeEvent(paths, {
          elapsed_ms: Math.round(performance.now() - captureStart),
          type: "canvas_visible",
        });
      })
      .catch(async (error: unknown) => {
        await writeEvent(paths, {
          elapsed_ms: Math.round(performance.now() - captureStart),
          error: errorMessage(error),
          type: "canvas_wait_failed",
        });
      });

    for (let index = 1; index <= runOptions.screenshotCount; index += 1) {
      const targetElapsed = (runOptions.durationMs / runOptions.screenshotCount) * index;
      await sleepUntil(captureStart + targetElapsed);
      const screenshotPath = path.join(
        paths.screenshotsDir,
        `second-${index.toString().padStart(2, "0")}.png`,
      );
      await page.screenshot({ path: screenshotPath });
      const sample: Sample = {
        cdp: await collectCdpProbe(cdp),
        dom: await collectDomProbe(page),
        elapsed_ms: Math.round(performance.now() - captureStart),
        screenshot_path: screenshotPath,
        second: index,
      };
      samples.push(sample);
      await writeEvent(paths, {
        sample,
        type: "sample",
      });
    }
    await canvasReady;

    await context.tracing.stop({ path: paths.tracePath });
    traceStopped = true;
    await writeEvent(paths, {
      trace_path: paths.tracePath,
      type: "trace_written",
    });

    const summary = buildSummary(runOptions, paths, samples);
    await writeFile(paths.summaryPath, `${JSON.stringify(summary, null, 2)}\n`);
    await writeRunPointer(runOptions, paths, {
      artifacts_dir: paths.runDir,
      events_path: paths.eventsPath,
      pid: process.pid,
      process_log_path: paths.processLogPath,
      run_id: runId,
      screenshot_dir: paths.screenshotsDir,
      started_at: summary.started_at,
      status: "completed",
      summary_path: paths.summaryPath,
      tail_command: tailCommand(runId),
      target_url: runOptions.url,
      trace_path: paths.tracePath,
    });
    await writeEvent(paths, {
      summary_path: paths.summaryPath,
      type: "run_completed",
    });
    console.log(JSON.stringify(summary, null, 2));
  } catch (error) {
    await writeEvent(paths, {
      error: errorMessage(error),
      type: "run_failed",
    });
    await writeFailedPointer(runOptions, paths);
    throw error;
  } finally {
    if (traceStarted && !traceStopped && context) {
      try {
        await context.tracing.stop({ path: paths.tracePath });
        await writeEvent(paths, {
          trace_path: paths.tracePath,
          type: "trace_written_after_failure",
        });
      } catch (error) {
        await writeEvent(paths, {
          error: errorMessage(error),
          type: "trace_stop_failed",
        });
      }
    }
    await browser?.close();
  }
}

function browserLaunchArgs(): string[] {
  return [
    "--disable-background-timer-throttling",
    "--disable-backgrounding-occluded-windows",
    "--disable-renderer-backgrounding",
    "--enable-gpu",
    "--ignore-gpu-blocklist",
  ];
}

async function tailEvents(options: HarnessOptions): Promise<void> {
  const pointer = loadPointer(options);
  let offset = 0;
  while (true) {
    if (existsSync(pointer.events_path)) {
      const stats = statSync(pointer.events_path);
      if (stats.size > offset) {
        const fileBytes = readFileSync(pointer.events_path);
        process.stdout.write(fileBytes.subarray(offset).toString("utf8"));
        offset = fileBytes.length;
      }
    }

    const latest = loadPointer(options);
    const runIsDone = latest.status === "completed" || latest.status === "failed";
    if (runIsDone && offset >= statSize(pointer.events_path)) {
      return;
    }
    await sleep(500);
  }
}

async function printStatus(options: HarnessOptions): Promise<void> {
  const pointer = loadPointer(options);
  const summary = existsSync(pointer.summary_path)
    ? JSON.parse(await readFile(pointer.summary_path, "utf8"))
    : null;
  console.log(
    JSON.stringify(
      {
        process_alive: pointer.pid > 0 ? isProcessAlive(pointer.pid) : false,
        run: pointer,
        summary,
      },
      null,
      2,
    ),
  );
}

async function runPreflight(
  options: HarnessOptions,
  paths: RunPaths,
  eventType: string,
): Promise<void> {
  const baseStart = performance.now();
  const base = await fetchWithTimeout(options.baseUrl, options.preflightTimeoutMs);
  await writeEvent(paths, {
    duration_ms: Math.round(performance.now() - baseStart),
    status: base.status,
    type: `${eventType}_base_url`,
    url: base.url,
  });
  if (base.status < 200 || base.status >= 500) {
    throw new Error(`dev server preflight failed: ${options.baseUrl} returned ${base.status}`);
  }

  const targetStart = performance.now();
  const target = await fetchWithTimeout(options.url, options.preflightTimeoutMs);
  await writeEvent(paths, {
    duration_ms: Math.round(performance.now() - targetStart),
    status: target.status,
    type: `${eventType}_target_url`,
    url: target.url,
  });
  if (target.status < 200 || target.status >= 500) {
    throw new Error(`target preflight failed: ${options.url} returned ${target.status}`);
  }
}

async function fetchWithTimeout(rawUrl: string, timeoutMs: number): Promise<Response> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await fetch(rawUrl, {
      redirect: "follow",
      signal: controller.signal,
    });
  } finally {
    clearTimeout(timer);
  }
}

function watchPage(page: Page, paths: RunPaths): void {
  page.on("console", (message) => {
    void writeEvent(paths, {
      level: message.type(),
      text: message.text(),
      type: "browser_console",
    });
  });
  page.on("pageerror", (error) => {
    void writeEvent(paths, {
      message: error.message,
      stack: error.stack ?? null,
      type: "page_error",
    });
  });
  page.on("requestfailed", (request) => {
    void writeEvent(paths, {
      failure: request.failure()?.errorText ?? null,
      type: "request_failed",
      url: request.url(),
    });
  });
  page.on("response", (response) => {
    const status = response.status();
    if (status < 400) {
      return;
    }
    void writeEvent(paths, {
      status,
      type: "http_error_response",
      url: response.url(),
    });
  });
}

async function collectDomProbe(page: Page): Promise<DomProbe> {
  return (await page.evaluate(`(() => {
    const textFor = (id) => document.getElementById(id)?.textContent?.trim() ?? "";
    const numberFor = (id) => {
      const value = Number.parseFloat(textFor(id).replaceAll(",", ""));
      return Number.isFinite(value) ? value : null;
    };
    const text = document.body.textContent ?? "";
    const hash = Array.from(new Uint8Array(new TextEncoder().encode(text)))
      .reduce((state, byte) => (state * 33 + byte) >>> 0, 5381)
      .toString(16)
      .padStart(8, "0");
    return {
      body_text_hash: hash,
      canvas_count: document.querySelectorAll("canvas").length,
      device_pixel_ratio: window.devicePixelRatio,
      draws: numberFor("guardian-glow-draws-value"),
      dpr_text: textFor("guardian-glow-dpr-value"),
      fps: numberFor("guardian-glow-fps-value"),
      frame_ms: numberFor("guardian-glow-frame-value"),
      pixels_text: textFor("guardian-glow-pixels-value"),
      probe_count: numberFor("guardian-glow-probe-count-value"),
      probe_ms: numberFor("guardian-glow-probe-ms-value"),
      probe_rate_text: textFor("guardian-glow-probe-rate-value"),
      probe_res_text: textFor("guardian-glow-probe-res-value"),
      programs: numberFor("guardian-glow-programs-value"),
      title: document.title,
      triangles: numberFor("guardian-glow-triangles-value"),
      url: window.location.href,
      viewport: {
        height: window.innerHeight,
        width: window.innerWidth,
      },
    };
  })()`)) as DomProbe;
}

async function collectCdpProbe(cdp: CDPSession): Promise<CdpProbe> {
  const response = (await cdp.send("Performance.getMetrics")) as {
    readonly metrics: readonly { readonly name: string; readonly value: number }[];
  };
  const metrics = new Map(response.metrics.map((metric) => [metric.name, metric.value]));
  return {
    dom_nodes: metric(metrics, "Nodes"),
    js_heap_total_mb: bytesToMb(metric(metrics, "JSHeapTotalSize")),
    js_heap_used_mb: bytesToMb(metric(metrics, "JSHeapUsedSize")),
    layout_count: metric(metrics, "LayoutCount"),
    layout_duration_ms: secondsToMs(metric(metrics, "LayoutDuration")),
    recalc_style_count: metric(metrics, "RecalcStyleCount"),
    script_duration_ms: secondsToMs(metric(metrics, "ScriptDuration")),
    task_duration_ms: secondsToMs(metric(metrics, "TaskDuration")),
  };
}

function metric(metrics: ReadonlyMap<string, number>, key: string): number | null {
  const value = metrics.get(key);
  return value === undefined ? null : round(value, 3);
}

function bytesToMb(value: number | null): number | null {
  return value === null ? null : round(value / 1_048_576, 2);
}

function secondsToMs(value: number | null): number | null {
  return value === null ? null : round(value * 1_000, 2);
}

function buildSummary(options: HarnessOptions, paths: RunPaths, samples: readonly Sample[]) {
  const fpsValues = samples.map((sample) => sample.dom.fps).filter(isNumber);
  const frameMsValues = samples.map((sample) => sample.dom.frame_ms).filter(isNumber);
  const probeMsValues = samples.map((sample) => sample.dom.probe_ms).filter(isNumber);
  const taskDurationValues = samples.map((sample) => sample.cdp.task_duration_ms).filter(isNumber);
  return {
    artifacts_dir: paths.runDir,
    avg_fps: average(fpsValues),
    avg_frame_ms: average(frameMsValues),
    avg_probe_ms: average(probeMsValues),
    avg_task_duration_ms: average(taskDurationValues),
    events_path: paths.eventsPath,
    max_frame_ms: max(frameMsValues),
    max_probe_ms: max(probeMsValues),
    min_fps: min(fpsValues),
    run_id: options.runId,
    sample_count: samples.length,
    samples,
    screenshot_dir: paths.screenshotsDir,
    started_at: new Date().toISOString(),
    target_url: options.url,
    trace_path: paths.tracePath,
    viewport: options.viewport,
  };
}

function average(values: readonly number[]): number | null {
  if (values.length === 0) {
    return null;
  }
  return round(values.reduce((sum, value) => sum + value, 0) / values.length, 2);
}

function min(values: readonly number[]): number | null {
  return values.length === 0 ? null : Math.min(...values);
}

function max(values: readonly number[]): number | null {
  return values.length === 0 ? null : Math.max(...values);
}

function isNumber(value: number | null): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function round(value: number, digits: number): number {
  const scale = 10 ** digits;
  return Math.round(value * scale) / scale;
}

function resolveRunPaths(options: HarnessOptions): RunPaths {
  const runDir = path.join(options.artifactsRoot, options.runId);
  return {
    eventsPath: path.join(runDir, "events.jsonl"),
    latestPath: path.join(options.artifactsRoot, "latest.json"),
    processLogPath: path.join(runDir, "process.log"),
    runDir,
    runJsonPath: path.join(runDir, "run.json"),
    screenshotsDir: path.join(runDir, "screenshots"),
    stderrPath: path.join(runDir, "stderr.log"),
    summaryPath: path.join(runDir, "summary.json"),
    tracePath: path.join(runDir, "trace.zip"),
  };
}

async function writeRunPointer(
  options: HarnessOptions,
  paths: RunPaths,
  pointer: RunPointer,
): Promise<void> {
  await mkdir(paths.runDir, { recursive: true });
  await writeFile(paths.runJsonPath, `${JSON.stringify(pointer, null, 2)}\n`);
  await mkdir(options.artifactsRoot, { recursive: true });
  await writeFile(paths.latestPath, `${JSON.stringify(pointer, null, 2)}\n`);
}

async function writeFailedPointer(options: HarnessOptions, paths: RunPaths): Promise<void> {
  await writeRunPointer(options, paths, {
    artifacts_dir: paths.runDir,
    events_path: paths.eventsPath,
    pid: process.pid,
    process_log_path: paths.processLogPath,
    run_id: options.runId,
    screenshot_dir: paths.screenshotsDir,
    started_at: new Date().toISOString(),
    status: "failed",
    summary_path: paths.summaryPath,
    tail_command: tailCommand(options.runId),
    target_url: options.url,
    trace_path: paths.tracePath,
  });
}

async function writeEvent(paths: RunPaths, event: Record<string, unknown>): Promise<void> {
  await mkdir(paths.runDir, { recursive: true });
  await appendFile(
    paths.eventsPath,
    `${JSON.stringify({
      at: new Date().toISOString(),
      ...event,
    })}\n`,
  );
}

function loadPointer(options: HarnessOptions): RunPointer {
  if (options.runId) {
    const paths = resolveRunPaths(options);
    return JSON.parse(readFileSync(paths.runJsonPath, "utf8")) as RunPointer;
  }
  const latestPath = path.join(options.artifactsRoot, "latest.json");
  return JSON.parse(readFileSync(latestPath, "utf8")) as RunPointer;
}

function serializeChildOptions(options: HarnessOptions): string[] {
  return [
    `--artifacts-dir=${options.artifactsRoot}`,
    `--base-url=${options.baseUrl}`,
    `--browser-executable=${options.browserExecutable}`,
    `--duration-ms=${options.durationMs}`,
    `--launch-timeout-ms=${options.launchTimeoutMs}`,
    `--navigation-timeout-ms=${options.navigationTimeoutMs}`,
    `--preflight-timeout-ms=${options.preflightTimeoutMs}`,
    `--run-id=${options.runId}`,
    `--screenshots=${options.screenshotCount}`,
    `--selector-timeout-ms=${options.selectorTimeoutMs}`,
    `--url=${options.url}`,
    `--viewport=${options.viewport.width}x${options.viewport.height}`,
  ];
}

function resolveVpBinary(): string {
  const appBinary = path.join(appRoot, "node_modules", ".bin", "vp");
  if (existsSync(appBinary)) {
    return appBinary;
  }
  return "vp";
}

function tailCommand(runId: string): string {
  return `pnpm --dir ${shellQuote(appRoot)} guardian:glow:tail -- --run-id=${shellQuote(runId)}`;
}

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

function createRunId(): string {
  return new Date().toISOString().replace(/[:.]/g, "-");
}

async function sleepUntil(deadline: number): Promise<void> {
  const waitMs = Math.max(0, deadline - performance.now());
  await sleep(waitMs);
}

async function sleep(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

function statSize(file: string): number {
  return existsSync(file) ? statSync(file).size : 0;
}

function isProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.stack ?? error.message;
  }
  return String(error);
}

function printHelp(): void {
  const runs = existsSync(defaultArtifactsRoot)
    ? readdirSync(defaultArtifactsRoot, { withFileTypes: true })
        .filter((entry) => entry.isDirectory())
        .map((entry) => entry.name)
        .slice(-3)
    : [];
  console.log(`Guardian glow harness

Commands:
  guardian:glow            Preflight, then start a background trace run.
  guardian:glow:run        Run the same capture in the foreground.
  guardian:glow:tail       Tail the latest run's structured JSONL events.
  guardian:glow:status     Print the latest run pointer and summary.

Options:
  --url=URL                Target page. Defaults to the local Guardian glow scene.
  --base-url=URL           Dev server preflight URL. Defaults to target origin.
  --duration-ms=5000       Capture window.
  --launch-timeout-ms=10000 Browser startup cap.
  --screenshots=5          Number of screenshots across the capture window.
  --viewport=1440x960      Browser viewport.
  --run-id=ID              Run id for run/tail/status.
  --browser-executable=PATH Chromium-compatible executable.

Recent runs: ${runs.length > 0 ? runs.join(", ") : "none"}`);
}

main().catch((error) => {
  console.error(errorMessage(error));
  process.exitCode = 1;
});
