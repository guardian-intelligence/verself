const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");

const GITHUB_ACTIONS_ISSUER = "https://token.actions.githubusercontent.com";
const CLOCK_SKEW_SECONDS = 60;

async function main() {
  const spec = readSpec();
  const token = await requestIDToken(spec.audience);
  mask(token);

  const jwt = decodeJWT(token);
  if (jwt.header.alg !== "RS256") {
    throw new Error(`expected RS256 OIDC token, got ${jwt.header.alg || "missing alg"}`);
  }
  if (!jwt.header.kid) {
    throw new Error("OIDC token header is missing kid");
  }

  const metadata = await fetchJSON(`${GITHUB_ACTIONS_ISSUER}/.well-known/openid-configuration`);
  if (metadata.issuer !== GITHUB_ACTIONS_ISSUER) {
    throw new Error(`unexpected OIDC issuer metadata: ${metadata.issuer}`);
  }
  if (!metadata.jwks_uri) {
    throw new Error("OIDC metadata is missing jwks_uri");
  }

  const jwks = await fetchJSON(metadata.jwks_uri);
  verifySignature(jwt, jwks);
  verifyClaims(jwt.payload, spec);

  setOutput("issuer", jwt.payload.iss);
  setOutput("subject", jwt.payload.sub);
  setOutput("audience", audienceToString(jwt.payload.aud));
  setOutput("repository", jwt.payload.repository);
  setOutput("runner-environment", jwt.payload.runner_environment || "");

  writeSummary(jwt.payload, spec);
  notice(
    `GitHub OIDC token verified for ${jwt.payload.repository} ` +
      `(aud=${audienceToString(jwt.payload.aud)}, sub=${jwt.payload.sub})`,
  );
}

function readSpec() {
  return {
    audience: input("audience") || "forge-metal-oidc-tracer",
    expectedRepository: input("expected-repository") || requiredEnv("GITHUB_REPOSITORY"),
    expectedRef: input("expected-ref") || requiredEnv("GITHUB_REF"),
    expectedSHA: (input("expected-sha") || requiredEnv("GITHUB_SHA")).toLowerCase(),
    expectedRunID: input("expected-run-id") || requiredEnv("GITHUB_RUN_ID"),
  };
}

async function requestIDToken(audience) {
  const requestURL = requiredEnv("ACTIONS_ID_TOKEN_REQUEST_URL");
  const requestToken = requiredEnv("ACTIONS_ID_TOKEN_REQUEST_TOKEN");
  const url = new URL(requestURL);
  url.searchParams.set("audience", audience);

  const body = await fetchJSON(url.toString(), {
    headers: {
      Accept: "application/json",
      Authorization: `bearer ${requestToken}`,
    },
  });
  if (!body.value || typeof body.value !== "string") {
    throw new Error("GitHub OIDC token response is missing value");
  }
  return body.value;
}

function decodeJWT(token) {
  const parts = token.split(".");
  if (parts.length !== 3) {
    throw new Error("OIDC token is not a JWT");
  }
  return {
    header: parseBase64URLJSON(parts[0], "header"),
    payload: parseBase64URLJSON(parts[1], "payload"),
    signingInput: `${parts[0]}.${parts[1]}`,
    signature: Buffer.from(parts[2], "base64url"),
  };
}

function parseBase64URLJSON(value, label) {
  try {
    return JSON.parse(Buffer.from(value, "base64url").toString("utf8"));
  } catch (error) {
    throw new Error(`could not decode JWT ${label}: ${error.message}`);
  }
}

function verifySignature(jwt, jwks) {
  const key = Array.isArray(jwks.keys) ? jwks.keys.find((candidate) => candidate.kid === jwt.header.kid) : null;
  if (!key) {
    throw new Error(`GitHub JWKS does not contain key ${jwt.header.kid}`);
  }
  const publicKey = crypto.createPublicKey({ key, format: "jwk" });
  const verifier = crypto.createVerify("RSA-SHA256");
  verifier.update(jwt.signingInput);
  verifier.end();
  if (!verifier.verify(publicKey, jwt.signature)) {
    throw new Error("GitHub OIDC token signature verification failed");
  }
}

function verifyClaims(payload, spec) {
  assertClaim(payload, "iss", GITHUB_ACTIONS_ISSUER);
  if (!audienceMatches(payload.aud, spec.audience)) {
    throw new Error(`unexpected audience claim: ${audienceToString(payload.aud)}`);
  }
  assertClaim(payload, "repository", spec.expectedRepository);
  assertClaim(payload, "ref", spec.expectedRef);
  assertClaim(payload, "sha", spec.expectedSHA);
  assertClaim(payload, "run_id", spec.expectedRunID);
  if (typeof payload.sub !== "string" || !payload.sub.startsWith(`repo:${spec.expectedRepository}:`)) {
    throw new Error(`unexpected subject claim: ${payload.sub || ""}`);
  }

  const now = Math.floor(Date.now() / 1000);
  if (!Number.isFinite(payload.exp) || payload.exp <= now - CLOCK_SKEW_SECONDS) {
    throw new Error("OIDC token is expired or missing exp");
  }
  if (payload.nbf !== undefined && Number(payload.nbf) > now + CLOCK_SKEW_SECONDS) {
    throw new Error("OIDC token nbf is in the future");
  }
  if (payload.iat !== undefined && Number(payload.iat) > now + CLOCK_SKEW_SECONDS) {
    throw new Error("OIDC token iat is in the future");
  }
}

function assertClaim(payload, name, expected) {
  const actual = payload[name];
  if (String(actual || "") !== String(expected || "")) {
    throw new Error(`unexpected ${name} claim: got ${String(actual || "")}, expected ${String(expected || "")}`);
  }
}

function audienceMatches(actual, expected) {
  if (Array.isArray(actual)) {
    return actual.includes(expected);
  }
  return actual === expected;
}

function audienceToString(value) {
  if (Array.isArray(value)) {
    return value.join(",");
  }
  return String(value || "");
}

async function fetchJSON(url, options = {}) {
  const response = await fetch(url, options);
  if (!response.ok) {
    throw new Error(`GET ${redactURL(url)} failed with HTTP ${response.status}: ${await response.text()}`);
  }
  return response.json();
}

function redactURL(raw) {
  const url = new URL(raw);
  const suffix = url.search ? "?<redacted>" : "";
  return `${url.origin}${url.pathname}${suffix}`;
}

function writeSummary(payload, spec) {
  if (!process.env.GITHUB_STEP_SUMMARY) {
    return;
  }
  const rows = [
    ["issuer", payload.iss],
    ["audience", audienceToString(payload.aud)],
    ["subject", payload.sub],
    ["repository", payload.repository],
    ["ref", payload.ref],
    ["sha", payload.sha],
    ["run_id", payload.run_id],
    ["runner_environment", payload.runner_environment || ""],
    ["job_workflow_ref", payload.job_workflow_ref || ""],
    ["expires_at", new Date(Number(payload.exp) * 1000).toISOString()],
  ];
  const lines = [
    "## Forge Metal OIDC tracer",
    "",
    "GitHub-issued OIDC token verified successfully on the Forge Metal runner.",
    "",
    "| Claim | Value |",
    "| --- | --- |",
    ...rows.map(([key, value]) => `| ${escapeMarkdown(key)} | ${escapeMarkdown(String(value || ""))} |`),
    "",
    `Requested audience: \`${escapeMarkdown(spec.audience)}\``,
    "",
  ];
  fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, lines.join(os.EOL));
}

function escapeMarkdown(value) {
  return value.replaceAll("|", "\\|").replaceAll("\n", " ");
}

function input(name) {
  return (process.env[`INPUT_${name.replaceAll("-", "_").toUpperCase()}`] || "").trim();
}

function requiredEnv(name) {
  const value = (process.env[name] || "").trim();
  if (!value) {
    if (name.startsWith("ACTIONS_ID_TOKEN_")) {
      throw new Error(`${name} is required; set workflow permissions.id-token: write`);
    }
    throw new Error(`${name} is required`);
  }
  return value;
}

function setOutput(name, value) {
  if (process.env.GITHUB_OUTPUT) {
    fs.appendFileSync(process.env.GITHUB_OUTPUT, `${name}=${value}${os.EOL}`);
  }
}

function notice(message) {
  console.log(`::notice::${message}`);
}

function mask(value) {
  if (value) {
    console.log(`::add-mask::${value}`);
  }
}

main().catch((error) => {
  console.error(`::error::${error.message}`);
  process.exitCode = 1;
});
