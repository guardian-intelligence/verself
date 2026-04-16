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
  await restore();
}

async function restore() {
  const spec = readSpec();
  saveState("key", spec.key);
  saveState("path", spec.path);
  fs.mkdirSync(spec.path, { recursive: true });

  const url = endpointURL("restore", spec.key);
  const response = await fetch(url, {
    method: "GET",
    headers: requestHeaders(),
  });
  if (response.status === 204 || response.status === 404) {
    notice(`Forge Metal sticky disk cache miss for ${spec.key}`);
    return;
  }
  if (!response.ok) {
    throw new Error(`restore failed: HTTP ${response.status}: ${await response.text()}`);
  }

  const tmp = path.join(os.tmpdir(), `forge-metal-stickydisk-${process.pid}-${Date.now()}.tgz`);
  try {
    const archive = Buffer.from(await response.arrayBuffer());
    fs.writeFileSync(tmp, archive, { mode: 0o600 });
    await run("tar", ["-xzf", tmp, "-C", spec.path]);
    notice(
      `Forge Metal sticky disk restored generation ${response.headers.get(
        "x-forge-metal-sticky-generation",
      ) ?? "unknown"} for ${spec.key}`,
    );
  } finally {
    fs.rmSync(tmp, { force: true });
  }
}

async function commit() {
  const key = process.env.STATE_key ?? "";
  const rawPath = process.env.STATE_path ?? "";
  if (!key || !rawPath) {
    notice("Forge Metal sticky disk skipped commit because restore state is missing");
    return;
  }
  const diskPath = expandPath(rawPath);
  if (!fs.existsSync(diskPath)) {
    notice(`Forge Metal sticky disk path does not exist: ${diskPath}`);
    return;
  }

  const tmp = path.join(os.tmpdir(), `forge-metal-stickydisk-${process.pid}-${Date.now()}.tgz`);
  try {
    await run("tar", ["-czf", tmp, "-C", diskPath, "."]);
    const archive = fs.readFileSync(tmp);
    if (archive.length === 0) {
      notice(`Forge Metal sticky disk archive is empty for ${key}`);
      return;
    }
    const response = await fetch(endpointURL("commit", key), {
      method: "POST",
      headers: {
        ...requestHeaders(),
        "Content-Type": "application/gzip",
      },
      body: archive,
    });
    if (!response.ok) {
      throw new Error(`commit failed: HTTP ${response.status}: ${await response.text()}`);
    }
    const body = await response.json();
    notice(`Forge Metal sticky disk committed generation ${body.generation} for ${key}`);
  } finally {
    fs.rmSync(tmp, { force: true });
  }
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

async function run(command, args) {
  await execFile(command, args, { stdio: "inherit" });
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

main().catch((error) => {
  console.error(`::error::${error.message}`);
  process.exitCode = 1;
});
