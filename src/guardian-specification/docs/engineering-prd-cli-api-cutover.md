# Guardian CLI API Cutover Engineering PRD

Status: Draft for implementation
Target PR: https://github.com/guardian-intelligence/verself/pull/135
Last updated: 2026-06-06

## Summary

Cut Guardian over from file-path-driven `board` and Bazelisk workflows to a
profile-driven command surface that can operate a site such as gamma end to
end:

```sh
guardian run bazel -- test //...
guardian preflight gamma
guardian fly gamma
guardian fly run gamma -- bazel test //...
```

The cutover makes Guardian the authority for repo-owned tool execution,
substrate preparation, deployment convergence, and verified post-convergence
remote tool runs. It removes `board` from the public API, makes profiles named
repo configuration instead of mutable local state, and replaces Bazelisk's
`.bazelversion` authority with a Guardian tool catalog of digest-pinned,
admitted artifacts.

## Problem

The current Guardian CLI exposes the implementation term `board` and accepts a
resource graph path as the primary operator input. That makes repeated site
operation noisy and hides the site context that an operator is actually
selecting.

Local tool execution also depends on Bazelisk for Bazel version management.
Bazelisk is useful bootstrap tooling, but it makes `.bazelversion` a tool
authority. Guardian needs stricter supply-chain control: tools must resolve
from repo-declared catalog entries, platform-specific artifact digests,
admission evidence, and content-addressed local caches.

Remote post-convergence commands are not yet modeled as verified Guardian tool
execution. If this is left as raw SSH-shaped behavior, `fly run` will become an
escape hatch that bypasses the same artifact and admission rules Guardian is
supposed to enforce.

## Goals

- Provide one compact CLI vocabulary for local verified tool execution,
  substrate preflight, convergence, and verified remote tool execution.
- Resolve Guardian configuration and profiles from repo-authored config by
  default.
- Keep profiles named contexts, not mutable selected workspaces.
- Make CUE the canonical configuration format for this repo.
- Prefer `.config/guardian` for authored config and `.guardian` for generated
  runtime state.
- Replace `guardian board` with `guardian preflight`.
- Make `guardian run bazel -- test //...` the Bazelisk replacement for normal
  development and CI validation.
- Keep `guardian fly run` catalog-only by default.
- Make arbitrary remote shell execution a separate audited breakglass command,
  not a mode hidden under `fly run`.
- Delete contradictory board/Bazelisk call sites and docs once stage-zero
  Guardian can build and run the repo-owned Bazel tool.

## Non-Goals

- No compatibility alias for `guardian board`.
- No hidden mutable command such as `guardian profile select`.
- No `.bazelversion` authority after the cutover.
- No `PATH` lookup or host package fallback for catalog tools.
- No profile-level `artifactPolicy`; Guardian always runs admitted,
  digest-pinned artifacts.
- No raw SSH command execution through `guardian fly run`.
- No new component operation fields in the base Guardian spec.
- No attempt to make profile selection part of `specdoc`; profile resolution is
  CLI/config behavior, not resource graph semantics.

## Users

- Operators running gamma/prod/dev recovery and deployment loops.
- Developers running repo-owned tools locally.
- CI jobs proving that the stage-zero Guardian binary can build and test the
  Guardian specification without Bazelisk as the operational entrypoint.

## Reference Models

- Bazel command UX: https://bazel.build/docs/user-manual
- Bazelisk version-selection role: https://bazel.build/install/bazelisk
- Kubernetes named contexts: https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/
- Kubernetes file-oriented `-f`: https://kubernetes.io/docs/reference/kubectl/generated/kubectl_apply/
- AWS named profiles: https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-files.html
- Docker Compose profile activation: https://docs.docker.com/compose/how-tos/profiles/
- Nomad job file operand: https://developer.hashicorp.com/nomad/docs/commands/job/run
- XDG config/cache/state/bin split: https://specifications.freedesktop.org/basedir-spec/0.8/

## Command Surface

Guardian must expose this command surface:

```text
guardian run <tool> -- <args...>
guardian tool list
guardian tool which <tool>
guardian tool verify <tool>
guardian tool install-shims --bin-dir <dir>
guardian profiles list
guardian profiles show [profile]
guardian preflight [-f <config>] [profile]
guardian fly [-f <config>] [profile]
guardian fly run [-f <config>] [profile] -- <tool> <args...>
```

`-f <config>` is the canonical file override. It follows the `kubectl`-style
operator convention that `-f` points at authored configuration. `--config
<config>` may exist as a readable long alias, but docs, examples, and tests
must prefer `-f`.

Normal usage selects a profile by name and discovers `.config/guardian` from
the workspace root. `-f` is for non-standard config files, explicit test
fixtures, or one-off documents.

The default profile is read from repo config:

```cue
defaultProfile: "gamma"
```

If a command requires a profile and no profile is provided, Guardian uses
`defaultProfile`. Commands must fail when no profile can be selected.

## CLI Semantics

### `guardian run`

`guardian run <tool> -- <args...>` runs a local verified catalog tool.

Required behavior:

- discover the workspace root;
- load the Guardian tool catalog;
- resolve `<tool>` for the current `GOOS/GOARCH` platform;
- require a digest-addressed artifact reference;
- require the artifact to be admitted;
- pull or reuse the content-addressed cache entry;
- verify the artifact digest before execution;
- locate the declared executable inside the cached artifact;
- execute the tool without consulting `PATH`;
- return the tool exit code.

Example:

```sh
guardian run bazel -- test //src/guardian-specification/...
```

### `guardian tool`

`guardian tool list` lists catalog tools available for the current platform.

`guardian tool which <tool>` prints the verified cached executable path after
resolving and verifying the tool.

`guardian tool verify <tool>` verifies that the catalog entry, digest,
admission status, cache artifact, and executable are usable.

`guardian tool install-shims --bin-dir <dir>` installs explicit user-local
shims. The command must require `--bin-dir`; it must not write to a global
directory by default. Shims point back to the Guardian binary and dispatch to
`guardian run <shim-name> -- <args...>`.

### `guardian profiles`

`guardian profiles list` lists repo-authored profiles and identifies the
default profile.

`guardian profiles show [profile]` shows the selected profile, resolved config
root, root config file, Guardian document path, and warnings.

Profiles are named operational contexts. They borrow the useful part of
kubeconfig contexts and AWS named profiles without adopting mutable current
context state.

### `guardian preflight`

`guardian preflight [-f <config>] [profile]` establishes and verifies the
relation between source, artifacts, credentials, and substrate. It replaces
`guardian board`.

Preflight may perform minimal idempotent preparation. It must disclose those
effects in output. It must not quietly mutate the remote host.

Required phases:

```text
ProfileLoaded
  -> ResourceGraphResolved
  -> FlyDocumentMaterialized
  -> LocalArtifactsPresent
  -> SubstrateConnected
  -> RemoteTreeMaterialized
  -> RemoteTreeVerified
  -> RemoteGuardianVerified
  -> OpenBaoInputsPrepared
  -> NomadActive
  -> KernelVerified
  -> ReadyToFly
```

Output must be structured and profile-oriented:

```yaml
profile: gamma
status: ready
conditions:
  - type: ProfileLoaded
    status: "True"
  - type: RemoteTreeVerified
    status: "True"
effects:
  - fly_document_materialized
  - remote_tree_materialized
  - openbao_inputs_prepared
```

`status` values are `ready` or `blocked`. A blocked preflight must include at
least one condition with a stable type, reason, and message.

### `guardian fly`

`guardian fly [-f <config>] [profile]` runs preflight, then converges deployment
state.

Required phases:

```text
ProfileLoaded
  -> PreflightReady
  -> NomadSubmitted
  -> ConvergenceObserved
  -> Ready
```

The first cutover may preserve today's Nomad hook behavior, but the condition
names and command output must describe the actual `fly` state machine. Later
iterations can make `ConvergenceObserved` consume component recovery reports,
Nomad scheduler evidence, health endpoints, and ClickHouse telemetry.

### `guardian fly run`

`guardian fly run [-f <config>] [profile] -- <tool> <args...>` converges the
target first, then runs a verified catalog tool through the remote Guardian
binary.

Required phases:

```text
ProfileLoaded
  -> PreflightReady
  -> FlyConverged
  -> RemoteGuardianVerified
  -> RemoteToolResolved
  -> RemoteToolVerified
  -> RemoteCommandStarted
  -> RemoteCommandExited
  -> ResultRecorded
```

Default behavior is catalog-only:

```sh
guardian fly run gamma -- nomad status
guardian fly run gamma -- bazel test //...
```

`fly run` must fail if convergence fails. It must not accept raw shell commands.

Arbitrary remote shell execution is a later, explicit, audited escape hatch:

```sh
guardian fly exec gamma --breakglass --reason "debug failed nomad client" -- /usr/bin/env ...
```

`fly exec` is out of scope for this cutover unless required to test emergency
recovery ergonomics. It must not be implemented as an alias or hidden mode of
`fly run`.

## Configuration Discovery

Guardian discovers authored config from the workspace root.

Preferred repo-local config root:

```text
.config/guardian/
```

Fallback for existing or simpler repos:

```text
.guardian/
```

Authored config and generated state must remain separate:

| Purpose | Location |
| --- | --- |
| Authored config | `.config/guardian/...` |
| Runtime/generated state | `.guardian/...` |
| Cache | `$XDG_CACHE_HOME/guardian/...`, defaulting to `$HOME/.cache/guardian/...` |
| Optional shims | explicit `--bin-dir`, normally `$HOME/.local/bin` |

Root config candidates, in priority order:

```text
guardian.cue
guardian.yaml
guardian.toml
```

Profile document candidates for profile `<name>`:

```text
.config/guardian/profiles/<name>.cue
.config/guardian/profiles/<name>.yaml
.config/guardian/profiles/<name>.toml
.config/guardian/profiles/<name>/guardian.cue
```

The same relative candidates apply under the `.guardian` fallback config root.

If multiple files exist at the same priority level, Guardian warns and chooses
by priority:

```text
warning: found multiple Guardian config files in .config/guardian; using guardian.cue, ignoring guardian.yaml
```

Missing profile errors must say exactly what was searched:

```text
guardian: profile "gamma" not found
searched:
  .config/guardian/profiles/gamma.cue
  .config/guardian/profiles/gamma.yaml
  .config/guardian/profiles/gamma.toml
  .config/guardian/profiles/gamma/guardian.cue
```

### Config Shape

The minimal root config is:

```cue
defaultProfile: "gamma"
```

A profile file may be the Guardian document itself. Avoid a second profile
object unless path indirection is needed.

Path indirection, when needed, must be explicit and minimal:

```cue
defaultProfile: "gamma"

profiles: gamma: {
	document: "../../src/guardian-specification/examples/gamma/gamma.cue"
}
```

No profile field may quietly change artifact policy. In particular,
`artifactPolicy` must not exist in the cutover API.

### `-f` File Override

`-f <config>` points at an authored Guardian configuration file. If the file is
a root Guardian config containing profiles, Guardian resolves the selected
profile relative to that file's directory. If the file is a standalone Guardian
document, Guardian runs that document directly.

Examples:

```sh
guardian preflight -f .config/guardian/guardian.cue gamma
guardian fly -f src/guardian-specification/examples/gamma/gamma.cue
```

`-f` must not write or select mutable local state. It only changes the file from
which this command resolves configuration.

### Config Resolution State Machine

```text
Start
  -> WorkspaceRootFound
  -> ConfigRootFound
  -> RootConfigLoaded
  -> ProfileSelected
  -> ProfileDocumentResolved
  -> GuardianDocumentLoaded
  -> Ready
```

Every terminal error must include the current state and the specific paths or
profile names involved.

## Tool Catalog

The tool catalog is repo-authored Guardian configuration. It owns declared tools
and platform selection.

Minimal shape:

```cue
tools: {
	bazel: {
		platforms: {
			"linux/amd64": {
				ref:        "oci.verself.sh/tools/bazel@sha256:..."
				executable: "bazel"
				admission:  "admitted"
			}
		}
	}
}
```

`ref` must be digest-addressed. Tags are not authority. Host PATH is not
authority. Host package managers are not authority.

### Tool Catalog State Machine

```text
ToolRequested
  -> CatalogLoaded
  -> ToolFound
  -> PlatformMatched
  -> DigestReferenceValidated
  -> Ready
```

Failure modes:

- catalog missing;
- tool missing;
- platform missing;
- ref is not digest-addressed;
- digest is malformed;
- artifact is not admitted.

## Tool Execution

Local execution is owned by `internal/toolrun`.

Required state machine:

```text
ToolResolved
  -> ArtifactPresentOrPulled
  -> DigestVerified
  -> AdmissionVerified
  -> ExecutableLocated
  -> ExecStarted
  -> ExecExited
```

First required target:

```sh
guardian run bazel -- test //...
```

This is the Bazelisk replacement. Bazelisk may remain only as temporary
bootstrap tooling to build the first Guardian binary until stage-zero Guardian
is published and documented. Once `guardian run bazel` proves it can test the
Guardian specification, docs and Aspect helper call sites should move to
Guardian.

## Resource Graph Scope

`internal/specdoc` remains focused on the Guardian resource graph. It must not
own profile selection or CLI config discovery.

The base spec may add only fields needed for portable remote execution.

Likely `Substrate.spec.remote` shape:

```cue
remote: {
	repoRoot:  "/home/ubuntu/.local/state/guardian/repo/current"
	guardian:  "/home/ubuntu/.local/state/guardian/repo/current/bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian"
}
```

If a transport field is required for the first `fly run` implementation, it
must be explicitly modeled as the command prefix for invoking the remote
Guardian binary. The remote command itself must be Guardian-mediated:

```text
<remote-transport> <guardian> tool verify <tool>
<remote-transport> <guardian> run <tool> -- <args...>
```

The base graph must not grow fields for raw command execution, restore,
initialize, unseal, wait, migrate, or component-specific convergence actions.

## Evidence and Output

Guardian command output is machine-readable and stable. Progress events may go
to stderr behind `--stream`; final command responses go to stdout.

Preflight and fly results must include:

- selected profile;
- selected Guardian document;
- status;
- stable conditions;
- disclosed idempotent effects;
- artifact digest or upload digest evidence where applicable;
- remote Guardian verification status when applicable.

`fly run` results must include:

- profile;
- tool name;
- remote Guardian path;
- remote repo root;
- command argv;
- exit code;
- start and end timestamps;
- result status;
- output streaming behavior;
- path to any structured result record.

Result recording may start as local JSON under `.guardian/fly/runs/`. Later
work can promote the same event shape into ClickHouse evidence.

## Security Requirements

- All catalog tools must be digest-pinned.
- All catalog tools must be admitted before execution.
- Unadmitted execution requires an explicit command flag or a separate
  breakglass command, not profile config.
- `guardian run` must not search `PATH`.
- `guardian fly run` must not accept raw shell commands.
- Remote command execution must invoke a verified remote Guardian binary.
- Shims must require an explicit `--bin-dir` and must not write to global
  system directories by default.
- Missing or contradictory config must fail loudly.
- Multiple config files must produce a warning that names both the chosen file
  and ignored files.
- Generated state must not be mixed into authored config.

## Implementation Plan

### Milestone 1: Config Discovery and Command Parser

Deliverables:

- Add `internal/guardianconfig`.
- Implement workspace root and config root discovery.
- Implement root config parsing for CUE, YAML, and TOML.
- Implement profile resolution and exact searched-path errors.
- Add `guardian profiles list`.
- Add `guardian profiles show`.
- Cut CLI parsing to profile operands and canonical `-f <config>` override.
- Add `.config/guardian/guardian.cue` with `defaultProfile: "gamma"`.

Acceptance:

- `guardian profiles list -o json` returns `gamma`.
- `guardian profiles show gamma -o json` returns the resolved Guardian
  document path.
- Missing profile tests assert the exact searched path list.

### Milestone 2: Preflight Cutover

Deliverables:

- Rename `evaluateBoard` to `evaluatePreflight`.
- Replace `guardian board` with `guardian preflight`.
- Rename board condition types to preflight vocabulary.
- Emit `profile` and `status` in command responses.
- Make `guardian preflight gamma --dry-run -o json` work from workspace root.
- Update Guardian docs, tests, benchmarks, examples, and command usage.
- Delete public `board` command support.

Acceptance:

- `rg "guardian board|\\bboard\\b|boarding" src/guardian-specification docs README.md`
  returns only historical references that are intentionally retained outside
  the cutover surface.
- `guardian preflight gamma --dry-run -o json` exits 0 and reports
  `status: ready` or a stable blocker.
- There is no compatibility alias for `board`.

### Milestone 3: Tool Catalog and Local Run

Deliverables:

- Add `internal/toolcatalog`.
- Add `internal/toolrun`.
- Add `.config/guardian/tools.cue`.
- Add `guardian tool list`.
- Add `guardian tool which <tool>`.
- Add `guardian tool verify <tool>`.
- Add `guardian tool install-shims --bin-dir <dir>`.
- Add `guardian run <tool> -- <args...>`.
- Add a Bazel catalog entry for the controller platform.

Acceptance:

- `guardian tool list -o json` includes `bazel`.
- `guardian tool which bazel -o json` resolves a content-addressed cached
  executable.
- `guardian tool verify bazel -o json` verifies digest, admission, and
  executable availability.
- `guardian run bazel -- test //src/guardian-specification/...` exits 0.
- Tests prove no PATH lookup is used for catalog execution.

### Milestone 4: Fly and Fly Run

Deliverables:

- Keep `guardian fly gamma` as preflight plus Nomad convergence.
- Add `guardian fly run gamma -- <tool> <args...>`.
- Verify remote Guardian before remote tool execution.
- Resolve and verify the remote tool through the remote Guardian catalog.
- Stream remote command output.
- Record a structured result.
- Add minimal `Substrate.spec.remote` fields needed by gamma.

Acceptance:

- `guardian fly gamma --dry-run -o json` exits 0 or reports a stable blocker.
- `guardian fly gamma -o json` reaches `status: ready` on a prepared gamma
  substrate.
- `guardian fly run gamma -- nomad status` verifies the remote Guardian and
  runs catalog `nomad`.
- `guardian fly run gamma -- bazel test //src/guardian-specification/...`
  verifies the remote Guardian and runs catalog `bazel`.
- `guardian fly run gamma -- /usr/bin/env` fails because raw commands are not
  catalog tools.

### Milestone 5: Bazelisk and Terminology Cleanup

Deliverables:

- Update README and developer docs from Bazelisk operational commands to
  Guardian tool commands after stage-zero Guardian is available.
- Remove Aspect helper call sites that invoke Bazelisk for ordinary repo tool
  execution.
- Delete `.bazelversion` or demote it to non-authoritative documentation only
  if a transition note is still needed.
- Rename benchmark directories and test names from board to preflight.
- Remove stale board terminology from Guardian docs and examples.

Acceptance:

- `rg "bazelisk|Bazelisk|\\.bazelversion"` returns only bootstrap notes that
  explicitly say Bazelisk is temporary stage-zero tooling, or returns no
  matches after full removal.
- `rg "guardian board|boarding|boarded|Board"` in Guardian runtime docs and
  code returns no public API surface.
- CI uses `guardian run bazel -- test //...` for Guardian validation once the
  stage-zero binary is available.

## Verification Plan

Run these commands before merging PR #135:

```sh
bazelisk build //src/guardian-specification/cli/cmd/guardian:guardian
./bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian profiles list -o json
./bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian profiles show gamma -o json
./bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian tool list -o json
./bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian tool verify bazel -o json
./bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian run bazel -- test //src/guardian-specification/...
./bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian preflight gamma --dry-run -o json
```

Live gamma verification, when credentials and substrate access are available:

```sh
./bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian preflight gamma -o json --stream
./bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian fly gamma -o json --stream
./bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian fly run gamma -- nomad status
./bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian fly run gamma -- bazel test //src/guardian-specification/...
```

Search gates:

```sh
rg "guardian board|boarding|boarded|Board" src/guardian-specification README.md docs
rg "bazelisk|Bazelisk|\\.bazelversion" README.md docs .aspect src
rg "artifactPolicy" .config src/guardian-specification
```

Search gates are pass/fail only after the relevant milestone. During the
stage-zero transition, Bazelisk references must be explicitly labeled as
temporary bootstrap authority.

## Rollout

This repo is pre-release, so the correct rollout is a full cutover, not
compatibility layering.

1. Land profile/config discovery and parser changes.
2. Cut `board` to `preflight` everywhere in Guardian code and docs.
3. Land the catalog-backed Bazel execution path.
4. Prove Guardian can run Bazel tests locally through `guardian run`.
5. Land `fly run` as catalog-only remote Guardian execution.
6. Remove Bazelisk operational call sites after stage-zero Guardian is usable.
7. Merge the verified branch directly into PR #135.

## Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| Config discovery ambiguity | deterministic priority order plus warnings naming chosen and ignored files |
| Accidental mutable profile state | no profile selection command; default profile is repo config |
| Supply-chain downgrade through PATH | no PATH lookup; catalog ref and digest are authority |
| Profile policy footguns | no `artifactPolicy`; admitted digest-pinned artifacts are the default rule |
| `fly run` becomes raw SSH | catalog-only command grammar; raw commands reserved for future audited `fly exec` |
| Stage-zero circularity | keep Bazelisk only to build the first Guardian binary until Guardian can run Bazel |
| Quiet remote mutation | preflight output includes explicit effects |
| Board terminology lingers | milestone search gates and docs cleanup before merge |

## Open Questions

- What is the first authoritative admission source for catalog tools during the
  stage-zero period: checked-in metadata, distribution-service, OCI referrers,
  or a temporary local admission document?
- Should `.bazelversion` be deleted immediately after `guardian run bazel`
  passes, or retained briefly as non-authoritative release-note style
  documentation?
- Should `Substrate.spec.remote` model SSH argv directly for the first
  implementation, or should it reference a future substrate transport resource?
- What is the durable result schema for `fly run` once local JSON records are
  promoted into ClickHouse evidence?

## Definition of Done

The cutover is done when:

- `guardian board` no longer exists.
- `guardian preflight gamma` is the substrate preparation command.
- `guardian fly gamma` is preflight plus convergence.
- `guardian fly run gamma -- <tool> ...` runs only verified catalog tools
  through a verified remote Guardian.
- `guardian run bazel -- test //src/guardian-specification/...` passes.
- public Guardian docs, tests, examples, and benchmarks use the new vocabulary.
- Bazelisk is no longer an operational tool authority.
- PR #135 contains the verified cutover without compatibility aliases or
  contradictory legacy paths.
