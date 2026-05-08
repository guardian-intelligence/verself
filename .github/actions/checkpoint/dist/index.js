const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFile: execFileCallback } = require("node:child_process");
const { promisify } = require("node:util");

const execFile = promisify(execFileCallback);

async function main() {
  const mountID = state("mount_id");
  if (mountID) {
    await saveCheckpoint(mountID);
    return;
  }

  const spec = readSpec();
  const mounted = await mountCheckpoint(spec);
  await vmBridgeMount(mounted);
  await checkpointPost("mounted", { mount_id: mounted.mount_id });

  saveState("mount_id", mounted.mount_id);
  setOutput("cache-hit", String(Boolean(mounted.cache_hit)));
  setOutput("mount-id", mounted.mount_id);
  setOutput("generation", String(mounted.source_generation || 0));
  notice(`Verself checkpoint mounted at ${mounted.mount_path} (cache_hit=${Boolean(mounted.cache_hit)}, generation=${mounted.source_generation || 0})`);
}

function readSpec() {
  const key = input("key");
  if (!key) {
    throw new Error("input key is required");
  }
  const requestedPath = input("path");
  if (!requestedPath) {
    throw new Error("input path is required");
  }
  return { key, mountPath: resolveMountPath(requestedPath) };
}

async function mountCheckpoint(spec) {
  return checkpointPost("mount", {
    key: spec.key,
    path: spec.mountPath,
  });
}

async function saveCheckpoint(mountID) {
  await checkpointPost("save", { mount_id: mountID });
  notice(`Verself checkpoint save requested for ${mountID}`);
}

async function checkpointPost(kind, body) {
  const response = await fetch(endpointURL(kind), {
    method: "POST",
    headers: requestHeaders(),
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    throw new Error(`checkpoint ${kind} failed: HTTP ${response.status}: ${await response.text()}`);
  }
  if (response.status === 204 || response.headers.get("content-length") === "0") {
    return {};
  }
  const text = await response.text();
  return text ? JSON.parse(text) : {};
}

async function vmBridgeMount(mounted) {
  for (const name of ["mount_id", "mount_name", "device_path", "mount_path"]) {
    if (!mounted[name]) {
      throw new Error(`checkpoint mount response missing ${name}`);
    }
  }
  await execFile("vm-bridge", [
    "filesystem",
    "mount",
    mounted.mount_name,
    mounted.device_path,
    mounted.mount_path,
    mounted.fs_type || "ext4",
  ], { maxBuffer: 1024 * 1024 });
}

function endpointURL(kind) {
  const origin = requiredEnv("VERSELF_HOST_SERVICE_HTTP_ORIGIN");
  const basePath = process.env.VERSELF_CHECKPOINT_PATH || "/internal/sandbox/v1/checkpoints";
  return new URL(`${basePath.replace(/\/$/, "")}/${kind}`, origin).toString();
}

function requestHeaders() {
  return {
    "Authorization": `Bearer ${requiredEnv("VERSELF_CHECKPOINT_TOKEN")}`,
    "Content-Type": "application/json",
    "X-Verself-Execution-Id": requiredEnv("VERSELF_EXECUTION_ID"),
    "X-Verself-Attempt-Id": requiredEnv("VERSELF_ATTEMPT_ID"),
  };
}

function resolveMountPath(value) {
  let resolved = value.trim();
  if (resolved.startsWith("~/")) {
    resolved = path.join(os.homedir(), resolved.slice(2));
  } else if (resolved === "~") {
    resolved = os.homedir();
  } else if (!path.isAbsolute(resolved)) {
    resolved = path.join(requiredEnv("GITHUB_WORKSPACE"), resolved);
  }
  return path.resolve(resolved);
}

function input(name) {
  return (process.env[`INPUT_${name.replaceAll("-", "_").toUpperCase()}`] || "").trim();
}

function state(name) {
  return (process.env[`STATE_verself_checkpoint_${name}`] || "").trim();
}

function requiredEnv(name) {
  const value = (process.env[name] || "").trim();
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}

function saveState(name, value) {
  if (process.env.GITHUB_STATE) {
    fs.appendFileSync(process.env.GITHUB_STATE, `verself_checkpoint_${name}=${value}${os.EOL}`);
  }
}

function setOutput(name, value) {
  if (process.env.GITHUB_OUTPUT) {
    fs.appendFileSync(process.env.GITHUB_OUTPUT, `${name}=${value}${os.EOL}`);
  }
}

function notice(message) {
  console.log(`::notice::${message}`);
}

main().catch((error) => {
  console.error(`::error::${error.message}`);
  process.exitCode = 1;
});
