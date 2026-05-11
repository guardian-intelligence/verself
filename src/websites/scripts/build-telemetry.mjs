#!/usr/bin/env node
import { spawn } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import fs from "node:fs";
import fsp from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import process from "node:process";

const serviceVersion = "0.1.0";
const defaultEndpoint = "https://analytics.api.verself.sh/v1/logs";
const defaultAudience = "https://analytics.api.verself.sh";

async function main() {
  const [command, ...args] = process.argv.slice(2);
  switch (command) {
    case "vp-run":
      process.exitCode = await runViteTask(args);
      break;
    case "tsc":
      process.exitCode = await runTypeScript(args);
      break;
    default:
      throw new Error("usage: build-telemetry.mjs <vp-run|tsc> ...");
  }
}

async function runViteTask(args) {
  const vpArgs = args[0] === "--" ? args.slice(1) : args;
  const workspaceRoot = findWorkspaceRoot(process.cwd());
  const queue = eventQueuePath();
  const startOffset = fileSize(queue);
  const started = Date.now();
  const exitCode = await spawnInherited("vp", ["run", "--verbose", ...vpArgs], {
    ...process.env,
    VERSELF_BUILD_TELEMETRY_PARENT: "1",
  });
  await emitViteTaskSummary(workspaceRoot, Date.now() - started, started);
  await flushQueueFromOffset(queue, startOffset, process.env.VERSELF_ANALYTICS_UPLOAD ?? "auto");
  return exitCode;
}

async function runTypeScript(args) {
  const startOffset = fileSize(eventQueuePath());
  const project = stringFlag(args, "--project") ?? stringFlag(args, "-p");
  if (!project) {
    throw new Error("build-telemetry tsc requires --project <tsconfig>");
  }
  const workspaceRoot = findWorkspaceRoot(process.cwd());
  const packageDir = findPackageDir(process.cwd());
  const packageName = packageNameForDir(packageDir);
  const projectAbs = path.resolve(process.cwd(), project);
  const projectRel = relativePath(workspaceRoot, projectAbs);
  const tsbuildInfoPath = tsBuildInfoPath(workspaceRoot, projectAbs);
  await fsp.mkdir(path.dirname(tsbuildInfoPath), { recursive: true });
  const before = statOrNull(tsbuildInfoPath);
  const started = Date.now();
  const result = await spawnCaptured("vp", [
    "exec",
    "tsc",
    "-p",
    project,
    "--incremental",
    "--tsBuildInfoFile",
    tsbuildInfoPath,
    "--extendedDiagnostics",
    "--pretty",
    "false",
  ]);
  const durationMs = Date.now() - started;
  const after = statOrNull(tsbuildInfoPath);
  const diagnostics = parseTypeScriptDiagnostics(result.output);
  const incrementalHit = Boolean(before && diagnostics.instantiationCount === 0);
  await recordEvent(workspaceRoot, {
    name: "build.typecheck",
    attributes: {
      "analytics.dataset": "build",
      "build.tool": "typescript",
      "build.package": packageName,
      "build.command": "typecheck",
      "build.config_path": projectRel,
      "typescript.version": typescriptVersion(workspaceRoot),
      "typescript.tsconfig_path": projectRel,
      "typescript.tsconfig_sha256": sha256File(projectAbs),
      "typescript.tsbuildinfo_path": relativeHomePath(tsbuildInfoPath),
      "typescript.tsbuildinfo.present_before": Boolean(before),
      "typescript.tsbuildinfo.present_after": Boolean(after),
      "typescript.tsbuildinfo.size_bytes_before": before?.size ?? 0,
      "typescript.tsbuildinfo.size_bytes_after": after?.size ?? 0,
      "typescript.tsbuildinfo.result": incrementalHit ? "hit" : "miss",
      "typescript.file_count": diagnostics.fileCount,
      "typescript.type_count": diagnostics.typeCount,
      "typescript.instantiation_count": diagnostics.instantiationCount,
      "typescript.check_ms": diagnostics.checkMs,
      "typescript.total_ms": diagnostics.totalMs,
      "cache.source": "typescript.tsbuildinfo",
      "cache.result": incrementalHit ? "hit" : "miss",
      duration_ms: durationMs,
      status: result.exitCode === 0 ? "succeeded" : "failed",
    },
  });
  if (process.env.VERSELF_BUILD_TELEMETRY_PARENT !== "1") {
    await flushQueueFromOffset(eventQueuePath(), startOffset, process.env.VERSELF_ANALYTICS_UPLOAD ?? "auto");
  }
  return result.exitCode;
}

async function emitViteTaskSummary(workspaceRoot, durationMs, startedMs) {
  const summaryPath = path.join(workspaceRoot, "node_modules", ".vite", "task-cache", "last-summary.json");
  const stat = statOrNull(summaryPath);
  if (!stat || stat.mtimeMs + 1000 < startedMs) {
    return;
  }
  const summary = await readJSON(summaryPath);
  if (!summary || !Array.isArray(summary.tasks)) {
    return;
  }
  for (const task of summary.tasks) {
    const cacheStatus = cacheStatusFromResult(task.result);
    const taskName = String(task.task_name ?? "");
    const command = String(task.command ?? taskName);
    const packageName = String(task.package_name ?? "");
    const packageDir = task.cwd ? String(task.cwd) : "";
    const event = eventForViteTask(taskName);
    await recordEvent(workspaceRoot, {
      name: event.name,
      attributes: {
        "analytics.dataset": "build",
        "build.tool": event.tool,
        "build.package": packageName,
        "build.command": taskName,
        "build.task_command": command,
        "build.package_dir": packageDir,
        "cache.source": "viteplus.task",
        "cache.result": cacheStatus,
        "cache.reason": cacheReason(task.result, cacheStatus),
        "viteplus.cache.source": "task-cache",
        "viteplus.cache.result": cacheStatus,
        "viteplus.cache.reason": cacheReason(task.result, cacheStatus),
        "viteplus.cache.saved_ms": Number(findNestedValue(task.result, "saved_duration_ms") ?? 0),
        "lint.runner": taskName === "lint" ? "vite-plus" : "",
        duration_ms: durationMs,
        status: taskSucceeded(task.result) ? "succeeded" : "failed",
      },
    });
  }
}

function eventForViteTask(taskName) {
  switch (taskName) {
    case "lint":
      return { name: "build.lint", tool: "oxlint" };
    case "format":
    case "fmt":
      return { name: "build.format", tool: "oxfmt" };
    case "build":
      return { name: "build.bundle", tool: "vite-plus" };
    case "typecheck":
    case "typecheck:scripts":
      return { name: "build.typecheck", tool: "vite-plus" };
    default:
      return { name: "build.task", tool: "vite-plus" };
  }
}

function parseTypeScriptDiagnostics(output) {
  return {
    fileCount: integerDiagnostic(output, "Files"),
    typeCount: integerDiagnostic(output, "Types"),
    instantiationCount: integerDiagnostic(output, "Instantiations"),
    checkMs: secondsDiagnostic(output, "Check time"),
    totalMs: secondsDiagnostic(output, "Total time"),
  };
}

function integerDiagnostic(output, label) {
  const match = output.match(new RegExp(`^${escapeRegExp(label)}:\\s+([0-9,]+)`, "m"));
  return match ? Number(match[1].replaceAll(",", "")) : 0;
}

function secondsDiagnostic(output, label) {
  const match = output.match(new RegExp(`^${escapeRegExp(label)}:\\s+([0-9.]+)s`, "m"));
  return match ? Math.round(Number(match[1]) * 1000) : 0;
}

async function recordEvent(workspaceRoot, event) {
  const queue = eventQueuePath();
  await fsp.mkdir(path.dirname(queue), { recursive: true });
  const timestamp = BigInt(Date.now()) * 1_000_000n;
  const record = {
    name: event.name,
    timeUnixNano: timestamp.toString(),
    attributes: sanitizeAttributes({
      "event.id": randomUUID(),
      "event.name": event.name,
      "service.name": "verself-build-telemetry",
      "service.version": serviceVersion,
      "github.job": process.env.GITHUB_JOB ?? "",
      ...event.attributes,
    }),
  };
  await fsp.appendFile(queue, `${JSON.stringify(record)}\n`, "utf8");
}

async function flushQueueFromOffset(queue, offset, mode) {
  const normalizedMode = mode === "required" || mode === "off" ? mode : "auto";
  if (normalizedMode === "off") {
    await truncateQueue(queue, offset);
    return;
  }
  const events = await readEventsFromOffset(queue, offset);
  if (events.length === 0) {
    return;
  }
  try {
    await uploadEvents(events);
    await truncateQueue(queue, offset);
    console.error(`verself-build-telemetry: uploaded ${events.length} events`);
  } catch (error) {
    if (normalizedMode === "required") {
      throw error;
    }
    await truncateQueue(queue, offset);
    console.error(`verself-build-telemetry: upload skipped: ${error.message}`);
  }
}

async function uploadEvents(events) {
  const endpoint = process.env.VERSELF_ANALYTICS_ENDPOINT ?? defaultEndpoint;
  const audience = process.env.VERSELF_ANALYTICS_AUDIENCE ?? defaultAudience;
  const token = await githubOIDCToken(audience);
  const payload = {
    resourceLogs: [
      {
        resource: {
          attributes: [
            otelAttribute("service.name", "verself-build-telemetry"),
            otelAttribute("service.version", serviceVersion),
          ],
        },
        scopeLogs: [
          {
            scope: {
              name: "verself.build.telemetry",
              version: serviceVersion,
            },
            logRecords: events.map((event) => ({
              timeUnixNano: event.timeUnixNano,
              observedTimeUnixNano: (BigInt(Date.now()) * 1_000_000n).toString(),
              body: { stringValue: event.name },
              attributes: Object.entries(event.attributes).map(([key, value]) => otelAttribute(key, value)),
            })),
          },
        ],
      },
    ],
  };
  const response = await fetch(endpoint, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    const body = await response.text();
    throw new Error(`analytics ingest failed with HTTP ${response.status}: ${body.slice(0, 200)}`);
  }
}

async function githubOIDCToken(audience) {
  const requestURL = process.env.ACTIONS_ID_TOKEN_REQUEST_URL;
  const requestToken = process.env.ACTIONS_ID_TOKEN_REQUEST_TOKEN;
  if (!requestURL || !requestToken) {
    throw new Error("GitHub Actions OIDC environment is unavailable");
  }
  const url = new URL(requestURL);
  url.searchParams.set("audience", audience);
  const response = await fetch(url, {
    headers: {
      Authorization: `Bearer ${requestToken}`,
    },
  });
  if (!response.ok) {
    throw new Error(`GitHub OIDC token request failed with HTTP ${response.status}`);
  }
  const body = await response.json();
  if (!body.value) {
    throw new Error("GitHub OIDC token response did not include value");
  }
  return body.value;
}

function otelAttribute(key, value) {
  if (typeof value === "boolean") {
    return { key, value: { boolValue: value } };
  }
  if (typeof value === "number" && Number.isFinite(value)) {
    if (Number.isInteger(value)) {
      return { key, value: { intValue: String(value) } };
    }
    return { key, value: { doubleValue: value } };
  }
  return { key, value: { stringValue: String(value ?? "") } };
}

function sanitizeAttributes(attributes) {
  const out = {};
  for (const [key, value] of Object.entries(attributes)) {
    if (value === undefined || value === null) {
      continue;
    }
    if (typeof value === "string" && value.length > 2048) {
      out[key] = value.slice(0, 2048);
      continue;
    }
    out[key] = value;
  }
  return out;
}

async function spawnCaptured(command, args) {
  return new Promise((resolve) => {
    const child = spawn(command, args, { stdio: ["inherit", "pipe", "pipe"] });
    let output = "";
    child.stdout.on("data", (chunk) => {
      process.stdout.write(chunk);
      output += chunk.toString("utf8");
    });
    child.stderr.on("data", (chunk) => {
      process.stderr.write(chunk);
      output += chunk.toString("utf8");
    });
    child.on("error", (error) => {
      output += `${error.message}\n`;
      resolve({ exitCode: 1, output });
    });
    child.on("close", (code) => resolve({ exitCode: code ?? 1, output }));
  });
}

async function spawnInherited(command, args, env) {
  return new Promise((resolve) => {
    const child = spawn(command, args, { stdio: "inherit", env });
    child.on("error", () => resolve(1));
    child.on("close", (code) => resolve(code ?? 1));
  });
}

async function readEventsFromOffset(queue, offset) {
  if (!fs.existsSync(queue)) {
    return [];
  }
  const handle = await fsp.open(queue, "r");
  try {
    const stat = await handle.stat();
    if (stat.size <= offset) {
      return [];
    }
    const buffer = Buffer.alloc(stat.size - offset);
    await handle.read(buffer, 0, buffer.length, offset);
    return buffer
      .toString("utf8")
      .split("\n")
      .filter(Boolean)
      .map((line) => JSON.parse(line));
  } finally {
    await handle.close();
  }
}

async function truncateQueue(queue, offset) {
  if (fs.existsSync(queue)) {
    await fsp.truncate(queue, offset);
  }
}

function eventQueuePath() {
  return process.env.VERSELF_BUILD_TELEMETRY_QUEUE ?? path.join(os.homedir(), ".cache", "verself-web", "events", "build-events.ndjson");
}

function tsBuildInfoPath(workspaceRoot, projectAbs) {
  const cacheRoot = process.env.VERSELF_TSBUILDINFO_DIR ?? path.join(os.homedir(), ".cache", "verself-web", "tsbuildinfo");
  const projectRel = relativePath(workspaceRoot, projectAbs);
  const digest = createHash("sha256").update(projectRel).digest("hex").slice(0, 16);
  return path.join(cacheRoot, `${safePathSegment(projectRel)}-${digest}.tsbuildinfo`);
}

function safePathSegment(value) {
  return value.replace(/[^A-Za-z0-9_.-]+/g, "__");
}

function relativeHomePath(filePath) {
  const home = os.homedir();
  if (filePath.startsWith(home + path.sep)) {
    return `~/${relativePath(home, filePath)}`;
  }
  return filePath;
}

function findWorkspaceRoot(start) {
  return findUp(start, "pnpm-workspace.yaml");
}

function findPackageDir(start) {
  return findUp(start, "package.json");
}

function findUp(start, marker) {
  let current = path.resolve(start);
  while (true) {
    if (fs.existsSync(path.join(current, marker))) {
      return current;
    }
    const parent = path.dirname(current);
    if (parent === current) {
      throw new Error(`could not find ${marker} from ${start}`);
    }
    current = parent;
  }
}

function packageNameForDir(packageDir) {
  const data = JSON.parse(fs.readFileSync(path.join(packageDir, "package.json"), "utf8"));
  return data.name ?? relativePath(findWorkspaceRoot(packageDir), packageDir);
}

function typescriptVersion(workspaceRoot) {
  return readPackageVersion(path.join(workspaceRoot, "node_modules", "typescript", "package.json"));
}

function readPackageVersion(filePath) {
  try {
    const data = JSON.parse(fs.readFileSync(filePath, "utf8"));
    return data.version ?? "";
  } catch (error) {
    if (error.code === "ENOENT") {
      return "";
    }
    throw error;
  }
}

async function readJSON(filePath) {
  try {
    return JSON.parse(await fsp.readFile(filePath, "utf8"));
  } catch (error) {
    if (error.code === "ENOENT") {
      return null;
    }
    throw error;
  }
}

function stringFlag(args, name) {
  const eq = args.find((arg) => arg.startsWith(`${name}=`));
  if (eq) {
    return eq.slice(name.length + 1);
  }
  const index = args.indexOf(name);
  if (index >= 0 && index + 1 < args.length) {
    return args[index + 1];
  }
  return null;
}

function statOrNull(filePath) {
  try {
    return fs.statSync(filePath);
  } catch (error) {
    if (error.code === "ENOENT") {
      return null;
    }
    throw error;
  }
}

function fileSize(filePath) {
  return statOrNull(filePath)?.size ?? 0;
}

function sha256File(filePath) {
  return createHash("sha256").update(fs.readFileSync(filePath)).digest("hex");
}

function relativePath(from, to) {
  return path.relative(from, to).split(path.sep).join("/");
}

function normalizeCacheStatus(value) {
  const normalized = String(value ?? "").toLowerCase();
  if (normalized.includes("hit") || normalized === "cached") {
    return "hit";
  }
  if (normalized.includes("miss")) {
    return "miss";
  }
  if (normalized.includes("disabled")) {
    return "disabled";
  }
  return "unknown";
}

function cacheStatusFromResult(result) {
  if (result && typeof result === "object") {
    if (Object.prototype.hasOwnProperty.call(result, "CacheHit")) {
      return "hit";
    }
    if (Object.prototype.hasOwnProperty.call(result, "CacheMiss")) {
      return "miss";
    }
  }
  const cacheStatus = findNestedValue(result, "cache_status");
  if (cacheStatus && typeof cacheStatus === "object") {
    if (Object.prototype.hasOwnProperty.call(cacheStatus, "Hit")) {
      return "hit";
    }
    if (Object.prototype.hasOwnProperty.call(cacheStatus, "Miss")) {
      return "miss";
    }
    if (Object.prototype.hasOwnProperty.call(cacheStatus, "Disabled")) {
      return "disabled";
    }
  }
  return normalizeCacheStatus(cacheStatus);
}

function cacheReason(result, cacheStatus) {
  if (cacheStatus === "disabled") {
    return "task configuration";
  }
  const modified = findNestedValue(result, "input_modified_path");
  if (modified) {
    return "input modified";
  }
  return "";
}

function taskSucceeded(result) {
  const encoded = JSON.stringify(result ?? {});
  return encoded.includes('"Success"') || !encoded.includes('"Failure"');
}

function findNestedValue(value, key) {
  if (!value || typeof value !== "object") {
    return null;
  }
  if (Object.prototype.hasOwnProperty.call(value, key)) {
    return value[key];
  }
  for (const child of Object.values(value)) {
    const found = findNestedValue(child, key);
    if (found !== null && found !== undefined) {
      return found;
    }
  }
  return null;
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

main().catch((error) => {
  console.error(`verself-build-telemetry: ${error.message}`);
  process.exit(1);
});
