const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFile: execFileCallback } = require("node:child_process");
const { promisify } = require("node:util");

const execFile = promisify(execFileCallback);

async function main() {
  if (isPost()) {
    await commit();
    return;
  }
  await setup();
}

async function setup() {
  const started = Date.now();
  const spec = readSpec();
  const mounts = configuredMounts(spec);
  const results = [];

  for (const mount of mounts) {
    fs.mkdirSync(mount.path, { recursive: true });
    if (!isMountPoint(mount.path)) {
      throw new Error(
        `Verself setup-node expected sticky disk ${mount.key} at ${mount.path}. ` +
          "The control plane must provision setup-node sticky disks before VM boot.",
      );
    }
    const warm = directoryHasEntries(mount.path);
    results.push({ ...mount, warm });
    saveState(`mount_${results.length - 1}`, JSON.stringify(mount));
  }
  saveState("mount_count", String(results.length));

  const nodeVersion = await commandOutput("node", ["--version"]);
  assertNodeMatches(spec.nodeVersion, nodeVersion.trim());

  let packageManagerVersion = "";
  if (spec.packageManager === "pnpm") {
    await execFile("pnpm", ["config", "set", "store-dir", spec.storePath, "--global"], { maxBuffer: 1024 * 1024 });
    packageManagerVersion = (await commandOutput("pnpm", ["--version"])).trim();
    appendEnv("NPM_CONFIG_STORE_DIR", spec.storePath);
    appendEnv("npm_config_store_dir", spec.storePath);
  } else {
    throw new Error(`unsupported package-manager ${spec.packageManager}; this tracer bullet supports pnpm`);
  }

  prependPath("/opt/verself/nodejs/bin");
  const storeHit = results.find((result) => result.kind === "store")?.warm ?? false;
  const modulesHit = spec.nodeModules ? (results.find((result) => result.kind === "node_modules")?.warm ?? false) : true;
  setOutput("store-cache-hit", String(storeHit));
  setOutput("node-modules-cache-hit", String(modulesHit));
  setOutput("cache-hit", String(storeHit && modulesHit));
  notice(
    `Verself setup-node ready in ${Date.now() - started}ms ` +
      `(node ${nodeVersion.trim()}, ${spec.packageManager} ${packageManagerVersion}, ` +
      `store_hit=${storeHit}, node_modules_hit=${modulesHit})`,
  );
}

async function commit() {
  const count = Number(process.env.STATE_mount_count || "0");
  if (!Number.isFinite(count) || count <= 0) {
    notice("Verself setup-node skipped save because no setup-node sticky disks were mounted");
    return;
  }
  for (let i = 0; i < count; i++) {
    const raw = process.env[`STATE_mount_${i}`] || "";
    if (!raw) continue;
    let mount;
    try {
      mount = JSON.parse(raw);
    } catch (error) {
      warning(`Verself setup-node skipped malformed mount state ${i}: ${error.message}`);
      continue;
    }
    await requestSave(mount.key, mount.path);
  }
}

async function requestSave(key, mountPath) {
  if (!fs.existsSync(mountPath)) {
    warning(`Verself setup-node sticky disk path does not exist: ${mountPath}`);
    return;
  }
  if (!isMountPoint(mountPath)) {
    warning(`Verself setup-node sticky disk path is no longer mounted: ${mountPath}`);
    return;
  }
  const response = await fetch(endpointURL("save", key), {
    method: "POST",
    headers: {
      ...requestHeaders(),
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ key, path: mountPath }),
  });
  if (!response.ok) {
    warning(`Verself setup-node sticky disk save failed: HTTP ${response.status}: ${await response.text()}`);
    return;
  }
  const body = await response.json().catch(() => ({}));
  notice(`Verself setup-node queued ZFS save ${body.commit_id ?? "unknown"} for ${key}`);
}

function readSpec() {
  const workingDirectory = normalizeRelativePath(input("working-directory") || ".");
  const workspace = requiredEnv("GITHUB_WORKSPACE");
  const workingDirectoryAbs = realPathForMaybeMissing(path.resolve(workspace, workingDirectory));
  const packageManager = (input("package-manager") || "").trim().toLowerCase();
  const packageManagerSpec = packageManagerSpecFor(workingDirectoryAbs, packageManager);
  const lockPath = lockfilePath(workingDirectoryAbs, packageManager);
  const lockHash = sha256File(lockPath);
  return {
    repository: requiredEnv("GITHUB_REPOSITORY"),
    runnerClass: process.env.VERSELF_RUNNER_CLASS || "unknown",
    nodeVersion: normalizeNodeVersion(input("node-version")),
    packageManager,
    packageManagerSpec,
    workingDirectory,
    workingDirectoryAbs,
    cache: parseBoolean(input("cache") || "true"),
    nodeModules: parseBoolean(input("node-modules") || "false"),
    lockHash,
    storePath: packageManagerStorePath(packageManager),
  };
}

function configuredMounts(spec) {
  const mounts = [];
  if (spec.cache) {
    mounts.push({
      kind: "store",
      key: setupNodeStickyKey(spec, "store"),
      path: spec.storePath,
    });
  }
  if (spec.nodeModules) {
    mounts.push({
      kind: "node_modules",
      key: setupNodeStickyKey(spec, "node_modules"),
      path: realPathForMaybeMissing(path.join(spec.workingDirectoryAbs, "node_modules")),
    });
  }
  return mounts;
}

function setupNodeStickyKey(spec, kind) {
  return [
    "setup-node:v1",
    `repo=${spec.repository}`,
    `runner=${spec.runnerClass}`,
    `node=${spec.nodeVersion}`,
    `pm=${spec.packageManagerSpec}`,
    `workdir=${spec.workingDirectory}`,
    `lock=${spec.lockHash}`,
    kind,
  ].join(":");
}

function packageManagerSpecFor(workingDirectoryAbs, packageManager) {
  if (packageManager !== "pnpm") {
    throw new Error(`unsupported package-manager ${packageManager}; this tracer bullet supports pnpm`);
  }
  const packageJSONPath = path.join(workingDirectoryAbs, "package.json");
  let packageManagerSpec = packageManager;
  if (fs.existsSync(packageJSONPath)) {
    const parsed = JSON.parse(fs.readFileSync(packageJSONPath, "utf8"));
    if (typeof parsed.packageManager === "string" && parsed.packageManager.trim()) {
      packageManagerSpec = parsed.packageManager.trim();
    }
  }
  return packageManagerSpec;
}

function lockfilePath(workingDirectoryAbs, packageManager) {
  if (packageManager === "pnpm") {
    return path.join(workingDirectoryAbs, "pnpm-lock.yaml");
  }
  throw new Error(`unsupported package-manager ${packageManager}; this tracer bullet supports pnpm`);
}

function packageManagerStorePath(packageManager) {
  if (packageManager === "pnpm") {
    return path.join(os.homedir(), ".pnpm-store");
  }
  throw new Error(`unsupported package-manager ${packageManager}; this tracer bullet supports pnpm`);
}

function normalizeRelativePath(value) {
  const cleaned = path.posix.normalize(String(value).trim() || ".");
  if (cleaned.startsWith("../") || cleaned === ".." || path.posix.isAbsolute(cleaned)) {
    throw new Error("working-directory must stay inside GITHUB_WORKSPACE");
  }
  return cleaned === "." ? "." : cleaned.replace(/\/$/, "");
}

function normalizeNodeVersion(value) {
  const raw = String(value || "").trim().replace(/^v/i, "");
  if (!raw) {
    throw new Error("node-version is required");
  }
  return raw.replace(/\.x$/i, "");
}

function assertNodeMatches(requested, actual) {
  const cleanActual = actual.replace(/^v/i, "");
  if (requested.includes(".")) {
    if (cleanActual !== requested) {
      throw new Error(`node ${cleanActual} does not match requested ${requested}`);
    }
    return;
  }
  if (!cleanActual.startsWith(requested + ".")) {
    throw new Error(`node ${cleanActual} does not match requested major ${requested}`);
  }
}

function sha256File(filePath) {
  if (!fs.existsSync(filePath)) {
    throw new Error(`lockfile is required for setup-node cache: ${filePath}`);
  }
  return crypto.createHash("sha256").update(fs.readFileSync(filePath)).digest("hex");
}

function directoryHasEntries(dir) {
  try {
    return fs.readdirSync(dir).some((entry) => entry !== "lost+found");
  } catch {
    return false;
  }
}

function endpointURL(operation, key) {
  const origin = requiredEnv("VERSELF_HOST_SERVICE_HTTP_ORIGIN");
  const basePath = process.env.VERSELF_STICKY_PATH || "/internal/sandbox/v1/stickydisk";
  const url = new URL(basePath.replace(/\/$/, "") + "/" + operation, origin);
  url.searchParams.set("key", key);
  return url;
}

function requestHeaders() {
  return {
    Authorization: `Bearer ${requiredEnv("VERSELF_STICKY_TOKEN")}`,
    "X-Verself-Execution-Id": requiredEnv("VERSELF_EXECUTION_ID"),
    "X-Verself-Attempt-Id": requiredEnv("VERSELF_ATTEMPT_ID"),
  };
}

function isMountPoint(targetPath) {
  const resolved = path.resolve(targetPath);
  const entries = fs.readFileSync("/proc/self/mountinfo", "utf8").split("\n");
  for (const entry of entries) {
    if (!entry.trim()) continue;
    const fields = entry.split(" ");
    if (fields.length > 4 && decodeMountInfoPath(fields[4]) === resolved) {
      return true;
    }
  }
  return false;
}

function decodeMountInfoPath(value) {
  return value.replace(/\\([0-7]{3})/g, (_, octal) => String.fromCharCode(parseInt(octal, 8)));
}

function realPathForMaybeMissing(targetPath) {
  const suffix = [];
  let current = path.resolve(targetPath);
  while (!fs.existsSync(current)) {
    const parent = path.dirname(current);
    if (parent === current) {
      return path.resolve(targetPath);
    }
    suffix.unshift(path.basename(current));
    current = parent;
  }
  return path.join(fs.realpathSync.native(current), ...suffix);
}

async function commandOutput(command, args) {
  const { stdout } = await execFile(command, args, { maxBuffer: 1024 * 1024 });
  return stdout;
}

function input(name) {
  const exact = `INPUT_${name.replace(/ /g, "_").toUpperCase()}`;
  const underscore = `INPUT_${name.replace(/[- ]/g, "_").toUpperCase()}`;
  return (process.env[exact] || process.env[underscore] || "").trim();
}

function requiredEnv(name) {
  const value = (process.env[name] || "").trim();
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}

function parseBoolean(value) {
  return ["1", "true", "yes", "on"].includes(String(value).trim().toLowerCase());
}

function setOutput(name, value) {
  if (process.env.GITHUB_OUTPUT) {
    fs.appendFileSync(process.env.GITHUB_OUTPUT, `${name}=${value}${os.EOL}`);
  }
}

function appendEnv(name, value) {
  if (process.env.GITHUB_ENV) {
    fs.appendFileSync(process.env.GITHUB_ENV, `${name}=${value}${os.EOL}`);
  }
}

function prependPath(value) {
  if (process.env.GITHUB_PATH) {
    fs.appendFileSync(process.env.GITHUB_PATH, `${value}${os.EOL}`);
  }
}

function saveState(name, value) {
  if (process.env.GITHUB_STATE) {
    fs.appendFileSync(process.env.GITHUB_STATE, `${name}=${value}${os.EOL}`);
  }
}

function isPost() {
  return Boolean(process.env.STATE_mount_count);
}

function notice(message) {
  console.log(`::notice::${message}`);
}

function warning(message) {
  console.log(`::warning::${message}`);
}

main().catch((error) => {
  console.error(`::error::${error.message}`);
  process.exitCode = 1;
});
