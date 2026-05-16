# NPM Supply Chain Incident Runbook

An npm supply-chain incident is active when a package manager may execute attacker-controlled code during dependency resolution, install, build, test, or publish. Treat install-time malware as credential theft until evidence proves the affected package was never resolved or executed in any environment with secrets, write tokens, OIDC exchange authority, or durable runner state.

## Activation

Open an incident record with:

- incident ID and UTC activation time;
- upstream advisory URLs, maintainer issues, registry entries, and threat-intel reports;
- affected package selectors, version ranges, tarball hashes, file indicators, process indicators, network indicators, and first known malicious publish time;
- impacted surfaces: local developer workspaces, generic CI, trusted deploy lanes, Verdaccio cache, Bazel external repositories, golden zvols, host bootstrap tools, customer workload images, and published packages.

Declare an install freeze for Node workspaces until scope is known. During the freeze:

- no `vp add`, `vp update`, `vp install`, `pnpm install`, `npm install`, `npm ci`, `npx`, `pnpm dlx`, or `vp dlx` from developer workstations or CI;
- no deploy promotion from a workflow that refreshed Node dependencies after the incident window opened;
- no golden zvol promotion from branch/job/matrix keys whose setup path could have executed the affected package set;
- no Verdaccio upstream fetches for scopes in the affected package set.

## Evidence Preservation

Preserve suspected environments before cleanup. Capture immutable copies of:

- the lockfile, `package.json`, `.npmrc`, `pnpm-workspace.yaml`, and package manager version;
- `node_modules` package manifests for affected scopes;
- package manager store entries for affected tarballs;
- CI run metadata, process logs, network logs, and environment-secret injection logs;
- Verdaccio storage metadata and cached tarballs for affected packages;
- golden zvol identifiers for workflows that ran after the malicious publish window.

Do not delete caches, node modules, Verdaccio packages, or zvols until the evidence copy exists. The cleanup step is separate from the evidence step.

## Triage

Run the source scan from the repo root:

```bash
rg -n "(@tanstack/setup|github:tanstack/router#79ac49eedf774dd4b0cfa308722bc463cfe5885c|router_init\\.js|tanstack_runner\\.js|ab4fcadaec49c03278063dd269ea5eef82d24f2124a8e15d7b90f2fa8601266c|2ec78d556d696e208927cc503d48e4b5eb56b31abc2870c2ed2e98d6be27fc96)" .
```

Scan lockfiles for advisory versions:

```bash
rg -n "(@tanstack/(history@1\\.161\\.(9|12)|react-router@1\\.169\\.(5|8)|react-start@1\\.167\\.(68|71)|react-start-client@1\\.166\\.(51|54)|react-start-server@1\\.166\\.(55|58)|router-core@1\\.169\\.(5|8)|router-generator@1\\.166\\.(45|48)|router-plugin@1\\.167\\.(38|41)|router-utils@1\\.161\\.(11|14)|start-client-core@1\\.168\\.(5|8)|start-fn-stubs@1\\.161\\.(9|12)|start-plugin-core@1\\.169\\.(23|26)|start-server-core@1\\.167\\.(33|36)|start-storage-context@1\\.166\\.(38|41)|virtual-file-routes@1\\.161\\.(10|13)|zod-adapter@1\\.166\\.(12|15))|@draftauth/(client@0\\.2\\.1|core@0\\.13\\.1)|@draftlab/(auth@0\\.24\\.1|auth-router@0\\.5\\.1|db@0\\.16\\.1)|@taskflow-corp/cli@0\\.1\\.(26|27)|@tolka/cli@1\\.0\\.2)" src/websites/pnpm-lock.yaml src/infrastructure-components/verdaccio/runtime/package-lock.json
```

Scan installed trees without executing package manager code:

```bash
find src/websites/node_modules -name router_init.js -o -name tanstack_runner.js 2>/dev/null
rg -n "@tanstack/setup|github:tanstack/router#79ac49eedf774dd4b0cfa308722bc463cfe5885c" src/websites/node_modules 2>/dev/null
```

Classify the environment:

- `unresolved`: no lockfile, node_modules, Verdaccio, CI log, or zvol evidence references the advisory set;
- `resolved`: lockfile or cache contains advisory packages, but no install or script execution evidence exists;
- `executed-unprivileged`: affected install executed without secrets, `id-token: write`, package publish authority, GitHub write scopes, or deploy credentials;
- `executed-privileged`: affected install executed with any secret, OIDC exchange authority, package publish authority, repo write authority, deployment token, or durable golden-state write path;
- `published`: a package or artifact was produced after affected execution and may carry malware or stolen-token propagation effects.

## Credential Response

For `unresolved`, do not rotate credentials. Record the negative evidence and close after monitoring the advisory feed.

For `resolved`, remove the affected package versions from the lockfile and Verdaccio cache, rebuild from known-good lock input, and keep the install freeze until a clean install produces ClickHouse evidence.

For `executed-unprivileged`, rotate credentials that were present on the machine outside the workflow environment, including `~/.npmrc`, local GitHub credentials, local cloud credentials, and any operator profile loaded into the shell. Invalidate the runner filesystem and related golden zvols.

For `executed-privileged`, revoke or roll every credential reachable from the process environment or local filesystem:

- npm tokens and package publish automation;
- GitHub personal access tokens, GitHub App private keys, and installation tokens where logs show API use outside expected runner control paths;
- GitHub Actions environment secrets and repository/organization secrets exposed to the job;
- OIDC trust relationships that could exchange the job token for external authority;
- Cloudflare, Latitude.sh, Resend, Stripe, SOPS/Age, OpenBao bootstrap, and Verself service credentials reachable from the job;
- customer or internal product secrets resolved by `secrets-service` for the run.

For `published`, deprecate or remove the published package/artifact, publish a clean fixed version from a trusted lane, and audit downstream consumers. Treat valid provenance as build-origin evidence only; a compromised build step can still produce an attested malicious artifact.

## Verdaccio Recovery

Quarantine the mirror before cleanup:

- block upstream fetches for affected scopes at the mirror or host egress layer;
- snapshot Verdaccio storage for evidence;
- enumerate cached versions and tarball digests for affected package names;
- remove malicious cached tarballs only after evidence capture;
- re-enable upstream fetches only after pnpm lockfiles have exact safe pins, `minimumReleaseAge`, `blockExoticSubdeps`, `strictDepBuilds`, and reviewed lifecycle-script allowlists.

The hosted mirror is a containment boundary. Direct registry fallback in `.npmrc` is an incident exception and must be reverted before merge.

## Golden Zvol Recovery

Golden state can preserve malicious node modules, package manager stores, Bazel external repositories, and developer tool caches. For every affected workflow tuple `(organization, project, repo, target-branch, workflow-id, job-id, matrix-key)`:

- mark branch and pull-request golden volumes produced after the malicious publish window as untrusted;
- stop promotion for ambiguous branch/job/matrix tuples;
- roll the target branch back to the last green golden volume before the incident window, or force a cold rebuild from a clean lockfile;
- delete PR-derived golden volumes after evidence capture because PR writes must not poison target-branch state;
- record the invalidated zvol IDs and replacement zvol IDs in the incident record.

## Reopening Criteria

Resume normal work only when all of these are true:

- lockfiles contain no advisory versions or malicious package sources;
- installed dependency trees contain no malicious files, optional dependencies, hashes, or process indicators;
- generic CI has `permissions: contents: read` and no repository, organization, environment, npm, cloud, deployment, or Verself secrets;
- privileged workflows are gated by protected refs and GitHub Environments;
- `id-token: write` exists only on jobs with an explicit OIDC trust policy and a matching external allowlist;
- Verdaccio cache has no malicious tarballs for the advisory set;
- affected golden zvols have been invalidated or replaced;
- credential rotations are complete for every `executed-privileged` or `published` environment;
- targeted ClickHouse queries show clean deploy, dependency resolution, and install verification evidence.

## Current Incident Profile

The May 11, 2026 TanStack incident profile includes:

- malicious optional dependency: `@tanstack/setup` from `github:tanstack/router#79ac49eedf774dd4b0cfa308722bc463cfe5885c`;
- file indicators: `router_init.js` and `tanstack_runner.js`;
- payload hashes: `router_init.js` SHA-256 `ab4fcadaec49c03278063dd269ea5eef82d24f2124a8e15d7b90f2fa8601266c`, `tanstack_runner.js` SHA-256 `2ec78d556d696e208927cc503d48e4b5eb56b31abc2870c2ed2e98d6be27fc96`;
- compromised-version source of truth: the active upstream maintainer issue and StepSecurity advisory, because the affected package list was still growing at incident time.

Primary references:

- TanStack Router issue: <https://github.com/TanStack/router/issues/7383>
- StepSecurity advisory: <https://www.stepsecurity.io/blog/mini-shai-hulud-is-back-a-self-spreading-supply-chain-attack-hits-the-npm-ecosystem>
- pnpm workspace settings: <https://pnpm.io/10.x/settings>
- GitHub `GITHUB_TOKEN` permissions: <https://docs.github.com/en/actions/tutorials/authenticate-with-github_token>
- GitHub Actions OIDC: <https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-cloud-providers>
- npm CI/CD token guidance: <https://docs.npmjs.com/using-private-packages-in-a-ci-cd-workflow/>
