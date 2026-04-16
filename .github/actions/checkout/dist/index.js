const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFile: execFileCallback, spawn } = require("node:child_process");
const { promisify } = require("node:util");

const execFile = promisify(execFileCallback);

async function main() {
  const started = Date.now();
  const spec = readSpec();
  prepareTarget(spec.targetPath, spec.clean);

  const bundlePath = path.join(os.tmpdir(), `forge-metal-checkout-${process.pid}-${Date.now()}.bundle`);
  try {
    const downloadStarted = Date.now();
    const bundleMeta = await downloadBundle(spec, bundlePath);
    const downloadMs = Date.now() - downloadStarted;

    const checkoutStarted = Date.now();
    await materializeCheckout(spec, bundlePath);
    const checkoutMs = Date.now() - checkoutStarted;

    const head = await gitOutput(spec.targetPath, ["rev-parse", "HEAD"]);
    if (head.trim().toLowerCase() !== spec.sha) {
      throw new Error(`checked out ${head.trim()}, expected ${spec.sha}`);
    }
    await git(spec.targetPath, ["config", "--global", "--add", "safe.directory", spec.targetPath]);
    setOutput("commit", spec.sha);
    notice(
      `Forge Metal checkout ready in ${Date.now() - started}ms ` +
        `(download ${downloadMs}ms, git ${checkoutMs}ms, cache_hit=${bundleMeta.cacheHit}, bytes=${bundleMeta.sizeBytes})`,
    );
  } finally {
    fs.rmSync(bundlePath, { force: true });
  }
}

function readSpec() {
  const repository = input("repository") || requiredEnv("GITHUB_REPOSITORY");
  const ref = input("ref") || requiredEnv("GITHUB_REF");
  const sha = requiredEnv("GITHUB_SHA").trim().toLowerCase();
  if (!/^[0-9a-f]{40}$/.test(sha)) {
    throw new Error(`GITHUB_SHA must be a 40-character commit SHA, got ${sha}`);
  }
  const targetPath = resolveCheckoutPath(input("path") || ".");
  const clean = parseBoolean(input("clean") || "true");
  const fetchDepth = input("fetch-depth") || "1";
  if (fetchDepth !== "1") {
    throw new Error("forge-metal/checkout@v1 currently supports fetch-depth: 1 only");
  }
  if (parseBoolean(input("persist-credentials") || "false")) {
    notice("persist-credentials is accepted for compatibility; Forge Metal checkout does not persist credentials.");
  }
  const token = input("token");
  return { repository, ref, sha, targetPath, clean, token };
}

function prepareTarget(targetPath, clean) {
  if (clean) {
    fs.rmSync(targetPath, { recursive: true, force: true });
  }
  fs.mkdirSync(targetPath, { recursive: true });
}

async function downloadBundle(spec, bundlePath) {
  const url = endpointURL(spec);
  const response = await fetch(url, {
    method: "POST",
    headers: requestHeaders(),
    body: JSON.stringify({
      repository: spec.repository,
      ref: spec.ref,
      sha: spec.sha,
      github_token: spec.token,
    }),
  });
  if (!response.ok) {
    throw new Error(`checkout bundle failed: HTTP ${response.status}: ${await response.text()}`);
  }
  const bundle = Buffer.from(await response.arrayBuffer());
  fs.writeFileSync(bundlePath, bundle, { mode: 0o600 });
  return {
    cacheHit: response.headers.get("x-forge-metal-checkout-cache-hit") || "unknown",
    sizeBytes: response.headers.get("x-forge-metal-checkout-size-bytes") || String(bundle.length),
  };
}

async function materializeCheckout(spec, bundlePath) {
  await git(spec.targetPath, ["init"]);
  await git(spec.targetPath, ["remote", "add", "origin", `https://github.com/${spec.repository}.git`]).catch(async () => {
    await git(spec.targetPath, ["remote", "set-url", "origin", `https://github.com/${spec.repository}.git`]);
  });
  await indexPack(spec.targetPath, bundlePath);
  fs.writeFileSync(path.join(spec.targetPath, ".git", "shallow"), `${spec.sha}${os.EOL}`);
  await git(spec.targetPath, ["update-ref", "refs/forge-metal/checkout", spec.sha]);
  await git(spec.targetPath, ["checkout", "--force", "--detach", spec.sha]);
  await git(spec.targetPath, ["clean", "-ffdx"]);
}

function endpointURL(spec) {
  const origin = requiredEnv("FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN");
  const basePath = process.env.FORGE_METAL_CHECKOUT_PATH || "/internal/sandbox/v1/github-checkout";
  const url = new URL(basePath.replace(/\/$/, "") + "/bundle", origin);
  return url;
}

function requestHeaders() {
  return {
    Authorization: `Bearer ${requiredEnv("FORGE_METAL_CHECKOUT_TOKEN")}`,
    "Content-Type": "application/json",
    "X-Forge-Metal-Execution-Id": requiredEnv("FORGE_METAL_EXECUTION_ID"),
    "X-Forge-Metal-Attempt-Id": requiredEnv("FORGE_METAL_ATTEMPT_ID"),
  };
}

function resolveCheckoutPath(value) {
  if (path.isAbsolute(value)) {
    throw new Error("path must be relative to GITHUB_WORKSPACE");
  }
  const workspace = requiredEnv("GITHUB_WORKSPACE");
  const resolved = path.resolve(workspace, value);
  const workspaceRoot = path.resolve(workspace);
  if (resolved !== workspaceRoot && !resolved.startsWith(workspaceRoot + path.sep)) {
    throw new Error("path must stay inside GITHUB_WORKSPACE");
  }
  return resolved;
}

async function git(cwd, args) {
  await execFile("git", args, { cwd, maxBuffer: 8 * 1024 * 1024 });
}

async function gitOutput(cwd, args) {
  const { stdout } = await execFile("git", args, { cwd, maxBuffer: 8 * 1024 * 1024 });
  return stdout;
}

async function indexPack(cwd, packPath) {
  await new Promise((resolve, reject) => {
    const child = spawn("git", ["index-pack", "--stdin"], { cwd, stdio: ["pipe", "ignore", "pipe"] });
    let stderr = "";
    child.stderr.on("data", (chunk) => {
      stderr += chunk.toString("utf8");
    });
    child.on("error", reject);
    child.on("close", (code) => {
      if (code === 0) {
        resolve();
      } else {
        reject(new Error(`git index-pack exited ${code}: ${stderr.trim()}`));
      }
    });
    fs.createReadStream(packPath).on("error", reject).pipe(child.stdin);
  });
}

function input(name) {
  return (process.env[`INPUT_${name.replaceAll("-", "_").toUpperCase()}`] || "").trim();
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

function notice(message) {
  console.log(`::notice::${message}`);
}

main().catch((error) => {
  console.error(`::error::${error.message}`);
  process.exitCode = 1;
});
