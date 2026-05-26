# RFC: Generated Artifact Authority

Status: proposed

Owner: platform

## Summary

Verself hosted runners deliberately preserve ignored build and test artifacts in
durable workspaces that are part of golden artifacts. CI starts from the last
trusted successful filesystem state instead of cold bootstrapping the
repository on every run.

This means generated files in ignored directories cannot be treated as
disposable files that CI may delete to recover confidence. They are cacheable
infrastructure. They are also not semantic truth. A generated file may remain
on disk, but it must lose authority when the generator that produced it is
removed or when the current source graph no longer declares that file as an
allowed generated input.

The proposed invariant is:

> A file under a generated-artifact root may exist forever, but source code may
> depend on it only when a current generator manifest authorizes the path and,
> in strict mode, its content fingerprint.

The platform should default-deny reads from generated roots that are not
covered by current generated-artifact authority. Customers keep hot durable
workspaces; stale generated artifacts stop silently satisfying source imports.

## Triggering Failure Mode

The concrete incident was in the frontend SDK generation path.

1. A frontend SDK change added a generated IAM transport target:

   - a Bazel generator target under `src/viteplus-monorepo/packages/sdk`
   - generated output:
     `__generated_sources/src/__generated/<api>/client.gen.ts`
   - materialized ignored source-tree projection:
     `src/viteplus-monorepo/packages/sdk/src/__generated/<api>/client.gen.ts`

2. A later change removed that generator path and deleted:

   - the generator rule load
   - the generator target
   - the generated-sources filegroup
   - the `write_source_files` target that projected
     `src/__generated/<api>/client.gen.ts`
   - references to the generated-sources target
     in web build/check/test `generated_srcs`

3. At that same commit, source still imported the stale ignored projection:

   ```ts
   import { createClient } from "./__generated/<api>/client.gen.js";
   ```

4. `**/__generated/` is gitignored. The Verself checkout action intentionally
   preserves untracked and ignored workspace state. A durable workspace restored
   from a golden artifact could therefore build successfully even though no
   current Bazel target produced that file.

5. The active source dependency was fixed by pointing SDK code at the current
   OpenAPI-generated transport and restoring the owning generator input.

This was not a generator failing to clean its current output directory. It was
a source import that retained a dependency on a generated path after the
producing target was removed. The generated file was stale but still present in
the durable workspace.

## Local Evidence

Relevant repo mechanics:

- `.gitignore` ignores `**/__generated/`, `**/*.generated.ts`, and
  `**/routeTree.gen.ts`.
- `.github/actions/checkout` documents that Verself checkout preserves
  untracked build state instead of running `git clean -ffdx`.
- `src/viteplus-monorepo/viteplus_rules.bzl` materializes generated sources before
  running `vp check`, `vp test`, and `vp build` in the source tree.
- `write_source_files` and the local generated-source sync remove and replace
  the destinations they currently own, but they do not know about paths whose
  generator target has been deleted from the graph.

Additional orphan examples observed locally:

- `src/viteplus-monorepo/apps/verself-web/src/__generated/openapi-specs/identity-api/openapi-3.1.yaml`
  remained after the catalog moved to `iam-api`.
- stale generated web app SDK directories such as `projects-api` and
  `notifications-api` can remain even when the current app only declares
  `profile-api` as a generated OpenAPI client.

Those extra files were not active imports during this investigation, but they
show the same ownership gap.

## Why Not Delete Generated Artifacts

Deleting ignored outputs on checkout or before every build fights the product
model. A durable workspace inside a golden artifact is allowed to contain:

- package-manager stores
- compiler caches
- database seed state
- generated SDK output
- route trees
- framework caches
- test caches
- other rebuildable state customers intentionally keep hot

The correct boundary is authority, not existence. A stale file can remain on
disk as cache state, but it must not be accepted as a build input unless the
current repository state authorizes it.

This mirrors the cache contract in `docs/product/golden-environments.md`: cache
state accelerates work, but it is not semantic truth.

## Related Work

The useful prior art is spread across build systems and supply-chain
provenance.

### in-toto

in-toto models supply-chain steps with materials, products, and artifact rules.
Its rule language includes `CREATE`, `DELETE`, `MODIFY`, `ALLOW`, `DISALLOW`,
`REQUIRE`, and `MATCH`; the project recommends a final `DISALLOW *` rule for
most steps.

This is the closest published model to "default deny unexpected generated
files." A Verself generator step can declare which artifacts it creates, and a
later build step can declare which generated artifacts it is allowed to consume.

References:

- <https://github.com/in-toto/in-toto#artifact-rules>
- <https://www.usenix.org/system/files/sec19-torres-arias.pdf>

### SLSA Provenance

SLSA provenance gives the platform contract shape: a build definition, external
parameters, resolved dependencies, and subjects. It also recommends rejecting
unexpected external parameters.

For Verself, generated-artifact manifests are a narrower repo-level analogue:
they describe the generated inputs allowed to influence a build and can be
included in deploy provenance.

Reference: <https://slsa.dev/spec/v1.1/provenance>

### Build Systems a la Carte

This paper gives language for why the failure occurred. The build graph did not
contain the real dependency from `iam.ts` to a currently declared generator
output. The file existed outside the declared graph, so source-tree tooling
could read it.

References:

- <https://simon.peytonjones.org/build-systems-a-la-carte/>
- <https://ndmitchell.com/downloads/paper-build_systems_a_la_carte_theory_and_practice-21_apr_2020.pdf>

### Perfect Dependencies and Traced Builds

Rattle and related work argue for discovering dependencies by tracing execution
instead of relying only on manually declared dependency edges. A Linux
file-access trace for `vp build` would have caught a read under
`src/__generated/` and compared it with current generated-artifact authority.

References:

- <https://arxiv.org/abs/2007.12737>
- <https://arxiv.org/abs/2202.05328>

### Nix Content-Addressed Outputs

Nix shows the distinction between preserving cached objects and authorizing
their use. Store objects can exist, but downstream derivations are governed by
hashes, references, and purity constraints.

Verself should use the same idea for generated source projections: generated
files may remain cached in the workspace, but a later build should only consume
them when the current manifest authorizes the path and fingerprint.

References:

- <https://nix.dev/manual/nix/2.27/store/derivation/outputs/content-address.html>
- <https://reproducible.nixos.org/>

### Bazel Hermeticity

Bazel's declared-input model is already the local build authority. The failure
occurred where a source-tree tool ran outside a fully isolated declared-input
view. Bazel is still the right owner for declaring generator outputs; Verself
needs an additional policy layer for preserved source-tree projections.

Reference: <https://docs.bazel.build/versions/main/hermeticity.html>

## Proposed Model

Introduce generated-artifact authority as a first-class policy object.

A generator emits a manifest similar to:

```json
{
  "schema": "verself.generated-artifacts.v1",
  "owner": "//src/viteplus-monorepo/packages/sdk:openapi_clients",
  "generator": "@hey-api/openapi-ts",
  "inputs": [
    {
      "path": "src/services/iam-service/openapi/openapi-3.1.yaml",
      "digest": "sha256:..."
    }
  ],
  "outputs": [
    {
      "path": "src/viteplus-monorepo/packages/sdk/src/__generated/iam-api/index.ts",
      "digest": "sha256:...",
      "kind": "source_projection"
    }
  ]
}
```

Authority is granted by current manifests, not by filesystem presence.

Consumers that read generated roots must be checked against the union of current
manifests:

- `src/**/__generated/**`
- `**/*.generated.ts`
- `**/routeTree.gen.ts`
- service OpenAPI projections that are materialized into source trees
- future language-specific generated SDK directories

Policy result:

- allowed: path is listed in a current manifest and, in strict mode, digest
  matches
- denied: path exists but no current manifest authorizes it
- degraded: path is listed but digest differs and policy is still in warn mode

## Enforcement Options

### Static Import Scanner

Scan source files for imports that resolve into generated roots and compare
them with current manifests.

Pros:

- fast
- easy to run in CI and local checks
- catches the exact IAM SDK failure

Cons:

- language-specific
- misses dynamic filesystem reads
- misses framework discovery that happens outside imports

### Build-Time File Access Trace

Run build/check/test commands under a file-open trace and fail when a generated
path is read without authority.

Pros:

- language-agnostic
- catches dynamic reads and framework discovery
- aligns with "perfect dependencies" research

Cons:

- platform-specific implementation work
- higher runtime cost
- needs careful path normalization for sandbox and source-tree builds

Possible mechanisms:

- `strace`/`ptrace` for a first Linux prototype
- eBPF or fanotify for lower-overhead production tracing
- Landlock-style deny policy for direct enforcement where practical

### Bazel Manifest Rule

Teach generator macros to emit a manifest target alongside generated outputs.
The manifest becomes an input to source-tree actions that need generated files.

Pros:

- fits existing Bazel ownership
- works with `write_source_files`
- can feed SLSA/in-toto provenance later

Cons:

- only covers generators owned by repo macros until broader adoption
- still needs a reader-side check for source-tree tools

### Source-Tree Projection Owner Registry

Keep a checked-in or generated registry of source-tree projection roots:

```yaml
version: 1
generated_roots:
  - root: src/viteplus-monorepo/packages/sdk/src/__generated
    owners:
      - //src/viteplus-monorepo/packages/sdk:openapi_clients
  - root: src/viteplus-monorepo/apps/verself-web/src/__generated/openapi-specs
    owners:
      - //src/viteplus-monorepo/apps/verself-web:openapi_spec_copies
```

The registry says who may authorize paths under a root. It does not list every
output by hand. The owner target's generated manifest lists exact files.

Pros:

- understandable customer-facing contract
- gives a default-deny root boundary

Cons:

- another policy file to maintain
- must be generated or checked for drift to avoid becoming stale itself

## Customer Responsibility

Verself should not make customers delete artifacts from durable workspaces. It
should make customers declare which generated artifacts are allowed to
influence builds.

For customer repositories, the platform-facing contract can be:

1. Declare generated roots and owners in a small policy file, for example
   `.verself/generated-artifacts.yml`.
2. Ensure every build step that consumes generated source has a current
   generator owner or manifest.
3. Treat denied generated reads as a configuration error, not as a cache miss.

Example:

```yaml
version: 1

generated:
  - root: frontend/src/__generated
    owner: npm:generate-openapi-clients
    mode: enforce
  - root: frontend/src/routeTree.gen.ts
    owner: npm:generate-routes
    mode: enforce
```

Verself can provide policy templates for common stacks, but the repository owner
remains responsible for describing generated roots. The platform enforces and
reports the invariant.

## Rollout Plan

1. **Document and agent guidance.** Treat ignored generated artifacts as cached
   infrastructure. Do not fix stale generated imports by deleting cache state.

2. **Repo-local static check.** Add a fast check that scans TypeScript imports
   under `src/viteplus-monorepo` and fails when an import resolves under `__generated`
   without a current generated target owner.

3. **Manifest emission for Vite+ generators.** Extend `viteplus_openapi_clients`,
   `viteplus_openapi_spec_copies`, and `viteplus_route_tree` to emit generated
   artifact manifests.

4. **Generated-root owner registry.** Add a generated or checked policy file
   listing source-tree projection roots and owner targets.

5. **Warn mode in CI.** Report orphan generated artifacts and unauthorized
   generated reads without blocking deploys.

6. **Enforce for repo-owned deploy artifacts.** Once false positives are low,
   fail deployable artifacts that read unauthorized generated files.

7. **Customer-facing policy.** Expose the same model through
   `.verself/generated-artifacts.yml` for hosted runner customers.

8. **Trace-based enforcement.** Add Linux file-access tracing for source-tree
   tools that cannot be statically analyzed.

## Open Questions

- Should manifests be checked in, generated into Bazel outputs, or recorded in
  ClickHouse evidence only?
- Should path authority require digest equality in CI immediately, or should
  digest checks start as warnings?
- Should source-tree actions run through a filtered filesystem view of allowed
  generated files instead of only checking after the fact?
- What is the minimum customer policy that gives protection without making
  Verself feel like a new caching API?
- How do we handle framework-generated files that are both source inputs and
  local developer conveniences, such as route trees?

## Decision Bias

Keep golden artifacts. Govern their authority.

The platform should make it impossible for stale generated files to silently
stand in for a deleted generator, while preserving the speed and debuggability
of durable workspaces.
