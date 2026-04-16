const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

async function main() {
  if (isPost()) {
    await commit();
    return;
  }
  await verifyMounted();
}

async function verifyMounted() {
  const spec = readSpec();
  saveState("key", spec.key);
  saveState("path", spec.path);
  fs.mkdirSync(spec.path, { recursive: true });
  if (!isMountPoint(spec.path)) {
    throw new Error(
      `Forge Metal sticky disk ${spec.key} was not mounted at ${spec.path}. ` +
        "The runner should provision sticky disks before the VM boots.",
    );
  }
  notice(`Forge Metal sticky disk mounted for ${spec.key} at ${spec.path}`);
}

async function commit() {
  const key = process.env.STATE_key ?? "";
  const rawPath = process.env.STATE_path ?? "";
  if (!key || !rawPath) {
    notice("Forge Metal sticky disk skipped save because mount state is missing");
    return;
  }
  const diskPath = expandPath(rawPath);
  if (!fs.existsSync(diskPath)) {
    warning(`Forge Metal sticky disk path does not exist: ${diskPath}`);
    return;
  }
  if (!isMountPoint(diskPath)) {
    warning(`Forge Metal sticky disk path is no longer mounted: ${diskPath}`);
    return;
  }

  const response = await fetch(endpointURL("save", key), {
    method: "POST",
    headers: {
      ...requestHeaders(),
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ key, path: diskPath }),
  });
  if (!response.ok) {
    warning(`Forge Metal sticky disk save request failed: HTTP ${response.status}: ${await response.text()}`);
    return;
  }
  const body = await response.json().catch(() => ({}));
  notice(`Forge Metal sticky disk queued ZFS save ${body.commit_id ?? "unknown"} for ${key}`);
}

function readSpec() {
  const key = requiredInput("key");
  const rawPath = requiredInput("path");
  return {
    key,
    path: expandPath(rawPath),
  };
}

function endpointURL(operation, key) {
  const origin = requiredEnv("FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN");
  const basePath = process.env.FORGE_METAL_STICKY_PATH || "/internal/sandbox/v1/stickydisk";
  const url = new URL(basePath.replace(/\/$/, "") + "/" + operation, origin);
  url.searchParams.set("key", key);
  return url;
}

function requestHeaders() {
  return {
    Authorization: `Bearer ${requiredEnv("FORGE_METAL_STICKY_TOKEN")}`,
    "X-Forge-Metal-Execution-Id": requiredEnv("FORGE_METAL_EXECUTION_ID"),
    "X-Forge-Metal-Attempt-Id": requiredEnv("FORGE_METAL_ATTEMPT_ID"),
  };
}

function requiredInput(name) {
  return requiredEnv(`INPUT_${name.replaceAll("-", "_").toUpperCase()}`);
}

function requiredEnv(name) {
  const value = (process.env[name] || "").trim();
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}

function expandPath(value) {
  if (value === "~") {
    return os.homedir();
  }
  if (value.startsWith("~/")) {
    return path.join(os.homedir(), value.slice(2));
  }
  if (path.isAbsolute(value)) {
    return value;
  }
  return path.resolve(process.env.GITHUB_WORKSPACE || process.cwd(), value);
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

function saveState(name, value) {
  if (!process.env.GITHUB_STATE) {
    return;
  }
  fs.appendFileSync(process.env.GITHUB_STATE, `${name}=${value}${os.EOL}`);
}

function isPost() {
  return Boolean(process.env.STATE_key || process.env.STATE_path);
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
