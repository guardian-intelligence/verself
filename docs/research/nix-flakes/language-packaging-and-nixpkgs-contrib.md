# Language-Specific Packaging and Nixpkgs Contribution Workflow

Covers: `buildPythonPackage`/`buildPythonApplication` deep dive, Rust `rustPlatform.buildRustPackage`, JavaScript/Node.js beyond `buildNpmPackage`, Go `buildGoModule` non-obvious attributes, and the nixpkgs collaborative infrastructure (branch structure, ofborg/GitHub Actions CI, `nixpkgs-review`, `nix-update`, `pkgs/by-name` migration).

---

## Topic 1: Python Packaging (`buildPythonPackage` / `buildPythonApplication`)

### `buildPythonPackage` vs `buildPythonApplication`: The Key Difference

Both functions are wrappers around `stdenv.mkDerivation` with Python-specific hooks added. The difference is one of intent and dependency propagation:

**`buildPythonPackage`** — for reusable libraries:
- Runtime dependencies go in `propagatedBuildInputs`. Those dependencies are automatically added to `PYTHONPATH` of any derivation that lists the library in its `buildInputs`.
- Package name is prefixed with the Python interpreter version: e.g., `python3.11-requests`.
- The package becomes importable in `python.buildEnv` or `python.withPackages`.
- Suitable for anything that will be `import`ed by another package.

**`buildPythonApplication`** — for end-user executables:
- Runtime deps go in `buildInputs`, **not** `propagatedBuildInputs`. The modules are not propagated because consumers only care about the installed binary, not the importable modules.
- No interpreter version prefix on the name.
- When added to `environment.systemPackages`, the binary works but the Python modules are **not** exposed for importing.
- Suitable for CLI tools, daemons, and any package whose Python code is an implementation detail.

The practical consequence: if a user adds a `buildPythonApplication` package to `environment.systemPackages` and then tries to `import` its internal modules from their own Python script, it will fail. Only `buildPythonPackage`-built libraries with proper `propagatedBuildInputs` form importable closures.

### `pythonPackages.callPackage` Scope: Why You Can't Just Use `callPackage`

The nixpkgs Python ecosystem is a **fixpoint attribute set** keyed to a specific interpreter. `pkgs.python311Packages`, `pkgs.python312Packages`, etc. are each generated from `pkgs/top-level/python-packages.nix` via a fixpoint that provides the correct interpreter, site-packages path, and dependency resolution.

When you add a Python library using `pkgs.callPackage ./my-lib.nix {}`, the package receives nixpkgs top-level attributes as arguments — meaning it does NOT get `python3Packages.requests`, it would get the raw `requests` (which doesn't exist at the top level). The package is also built against the wrong Python version, and its `propagatedBuildInputs` don't wire into any interpreter's package set.

**Correct pattern — via `packageOverrides` in a flake:**

```nix
nixpkgs.overlays = [(final: prev: {
  python3 = prev.python3.override {
    packageOverrides = pyFinal: pyPrev: {
      mylib = pyPrev.buildPythonPackage {
        pname = "mylib";
        version = "1.0";
        src = ./src;
        propagatedBuildInputs = [ pyPrev.requests pyPrev.pydantic ];
      };
    };
  };
  # For the application, use python3.pkgs.mylib from above
  myapp = final.python3.pkgs.mylib;
})];
```

Once in the package set, `python3.pkgs.mylib` correctly propagates into any `python3.withPackages (ps: [ps.mylib])` call.

The nixpkgs convention: libraries live in `pkgs/development/python-modules/<name>/default.nix` and are listed in `pkgs/top-level/python-packages.nix`. Applications that happen to be Python-based but aren't libraries use `pkgs/by-name` with `python3Packages.callPackage` passed as an argument.

### `propagatedBuildInputs` and the Python Closure

For Python packages, `propagatedBuildInputs` is not optional for runtime deps — it is load-bearing:

- Any Python library that must be importable at runtime **must** be in `propagatedBuildInputs`.
- If a dep is in `buildInputs` only, it is available during the build phase but is **not** added to the wrapper script's `PYTHONPATH`. Import will fail at runtime.
- Corresponds to `install_requires` in `setup.py` / `[project.dependencies]` in `pyproject.toml`.
- `buildInputs` is for C libraries linked via FFI (e.g., `libxml2` for `lxml`). The C library is linked in; it does not need to be on `PYTHONPATH`.
- `nativeCheckInputs` (previously `checkInputs`) is for test-only deps like `pytest`. These are available only during `checkPhase` and are not propagated.

### `nativeBuildInputs` for Build Tools

Python's `nativeBuildInputs` holds build-time executables — tools that must run on the build machine (not the target) during the build:

| Tool | Purpose |
|------|---------|
| `setuptools` | Legacy `setup.py` builds |
| `wheel` | Wheel building |
| `flit-core` | Flit build backend |
| `hatchling` | Hatch build backend (modern pyproject) |
| `poetry-core` | Poetry build backend |
| `cython` | Cython transpilation |
| `pythonRelaxDepsHook` | Relax strict version pins in wheel metadata |
| `pythonRemoveTestsDir` | Strip `tests/` from the installed package |

These correspond to `build-system.requires` in `pyproject.toml`.

### The `format` Attribute

The `format` attribute tells `buildPythonPackage` which build backend to invoke:

| Value | Meaning | Build mechanism |
|-------|---------|----------------|
| `"setuptools"` | Legacy `setup.py` | `python setup.py bdist_wheel` |
| `"pyproject"` | PEP 517/518 (any backend) | `pip wheel --no-build-isolation .` via the declared backend |
| `"flit"` | Flit directly | `flit build` |
| `"wheel"` | Pre-built `.whl` provided as `src` | `pip install --no-deps *.whl` |
| `"other"` | None of the above | Custom `buildPhase`/`installPhase` required |

As of 2024, `"pyproject"` is the recommended value for new packages since it delegates to whatever backend `pyproject.toml` declares and is backend-agnostic.

### `pythonImportsCheck`

`pythonImportsCheck` is a list of module names that are imported in a dedicated post-install phase to verify the package was installed correctly:

```nix
buildPythonPackage {
  pname = "cryptography";
  version = "41.0.0";
  # ...
  pythonImportsCheck = [
    "cryptography"
    "cryptography.hazmat.primitives"
  ];
}
```

This runs `python -c "import cryptography; import cryptography.hazmat.primitives"` in a sandboxed environment after installation but **independently of `doCheck`**. The check runs even when `doCheck = false`. It catches missing `propagatedBuildInputs` that test suites might not exercise (e.g., a dep only used in a rarely-triggered code path). This is now required by nixpkgs maintainers for most new Python package submissions.

### `pythonRelaxDepsHook` and `pythonRemoveTestsDir`

```nix
nativeBuildInputs = [ pythonRelaxDepsHook ];
# Remove version upper bounds from specific deps:
pythonRelaxDeps = [ "requests" "urllib3" ];
# Or relax ALL deps (nuclear option):
pythonRelaxDeps = true;
# Remove deps entirely from wheel metadata (converts build error to runtime error):
pythonRemoveDeps = [ "black" ];  # remove dev-only dep that leaked into install_requires
```

`pythonRelaxDepsHook` patches the wheel's `METADATA` or `PKG-INFO` to remove version upper bounds that conflict with nixpkgs. Use when upstream pins `requests<2.29` but nixpkgs only ships `requests==2.31`.

`pythonRemoveTestsDir` removes the `tests/` directory from the installed wheel to reduce closure size. Add it to `nativeBuildInputs`; no configuration attributes needed.

---

## Topic 2: Rust Packaging (`rustPlatform.buildRustPackage`)

### `cargoHash` vs `cargoLock`: Two Vendoring Strategies

**Strategy 1: `cargoHash` (FOD of the vendor directory)**

`buildRustPackage` runs a two-phase build. Phase 1 fetches all crate sources from `crates.io` into a vendor directory; phase 2 builds in `--offline` mode against that vendor directory. `cargoHash` is the SRI hash of the NAR of the vendor directory output from phase 1.

```nix
rustPlatform.buildRustPackage {
  pname = "ripgrep";
  version = "14.1.0";
  src = fetchFromGitHub { owner = "BurntSushi"; repo = "ripgrep"; rev = version; hash = "..."; };
  cargoHash = "sha256-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx=";
}
```

`cargoSha256` (legacy) accepts a bare SHA-256 hex string. `cargoHash` accepts SRI format (`sha256-...=`). Both are computed over the same vendor directory; `cargoHash` is the current nixpkgs convention.

To update: set `cargoHash = lib.fakeHash;`, build, read the correct hash from the error output.

**Strategy 2: `cargoLock` (use `Cargo.lock` directly)**

Added in nixpkgs ~23.05 (PR #122158). Instead of a hash of the vendor directory, you point directly at `Cargo.lock`. Nixpkgs fetches each crate listed in the lock file using individual FODs:

```nix
rustPlatform.buildRustPackage {
  pname = "my-app";
  version = "0.1.0";
  src = ./.;
  cargoLock = {
    lockFile = ./Cargo.lock;
  };
}
```

Advantages:
- No `cargoHash` to update when adding/removing deps — only `Cargo.lock` changes (which is checked in).
- Self-describing: the lock file IS the dependency specification.
- Easier for flake-based development where the source is local.

Disadvantage: `Cargo.lock` must be present in the source (it usually is for binaries, but may be absent for libraries).

### The `cargoLock.outputHashes` Pattern for Git Dependencies

When `Cargo.lock` contains `source = "git+https://..."` entries, the crate source cannot be fetched from `crates.io`. Each git dependency requires an explicit hash:

```nix
cargoLock = {
  lockFile = ./Cargo.lock;
  outputHashes = {
    "some-git-crate-0.1.0" = "sha256-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx=";
    "another-git-dep-0.2.3" = "sha256-yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy=";
  };
};
```

The key is `"<crate-name>-<version>"` as it appears in `Cargo.lock`. If a hash is missing, the build fails with a message listing exactly which crates need hashes — the error is actionable.

For local/flake usage (outside nixpkgs where IFD is acceptable), `allowBuiltinFetchGit = true` skips the `outputHashes` requirement and uses `builtins.fetchGit` to fetch git dependencies at eval time. This is disallowed in nixpkgs itself due to IFD restrictions (see [fods-ifd-and-internals.md](fods-ifd-and-internals.md)).

### Build Control Attributes

```nix
rustPlatform.buildRustPackage {
  # ...

  # Build features
  buildNoDefaultFeatures = true;        # pass --no-default-features
  buildFeatures = [ "color" "net" ];    # pass --features color,net

  # Features for check phase only (override buildFeatures for tests)
  checkNoDefaultFeatures = true;
  checkFeatures = [ "color" ];

  # Build type: "release" (default) or "debug"
  buildType = "debug";

  # Skip specific tests without disabling all tests
  checkFlags = [
    "--skip=integration_tests::slow_test"
    "--skip=network_tests"
  ];
  # For flags to cargo test itself (not the test binary):
  cargoTestFlags = [ "--workspace" ];

  # Disable parallel test execution (for tests that use ports, global state):
  dontUseCargoParallelTests = true;

  # Use cargo-nextest instead of cargo test:
  useNextest = true;
}
```

`CARGO_BUILD_INCREMENTAL` is not explicitly set by `buildRustPackage` to `"false"` as a named attribute, but incremental compilation is effectively meaningless in the Nix sandbox (no persistent build cache between builds) and the sandbox's read-only store prevents incremental artifact caching. If needed, set `env.CARGO_BUILD_INCREMENTAL = "false";` explicitly.

### Cross-Compilation: `pkgsCross`

```nix
# Build for aarch64-linux from x86_64-linux:
pkgsCross.aarch64-multiplatform.rustPlatform.buildRustPackage {
  pname = "my-app";
  # ... same attrs
}
```

Nixpkgs derives the Cargo target triple from `stdenv.hostPlatform.rust.rustcTargetSpec`. For standard targets this is automatic. For custom targets:

```nix
rustPlatform.buildRustPackage {
  CARGO_BUILD_TARGET = "thumbv7em-none-eabi";
  # Custom targets need a JSON spec:
  # rustc.platform = { os = "none"; arch = "arm"; ... };
  doCheck = false;  # custom targets can't run tests on the build host
}
```

---

## Topic 3: JavaScript/Node.js (Beyond `buildNpmPackage`)

### The `npmHooks` System

`buildNpmPackage` uses a hook system where each phase is a composable setup hook:

**`npmHooks.npmConfigHook`**: Runs before the build; configures npm to use the pre-fetched `npmDeps` store (the offline npm cache FOD). Sets `npm_config_cache` to point at the FOD. Without this, npm would try to fetch from the network.

**`npmHooks.npmBuildHook`**: Runs `npm run build` in the build phase. This is what triggers webpack, esbuild, tsc, etc. Respects `npmBuildScript` (default: `"build"`).

**`npmHooks.npmInstallHook`**: Runs `npm pack --json --dry-run` to determine which files npm would publish, then copies those files to `$out/lib/node_modules/$name/`. Creates `$out/bin/` symlinks for `bin` entries in `package.json`. This is why `buildNpmPackage` does **not** run `npm install` in the install phase — it uses `npm pack` semantics to copy only published files.

**`importNpmLock.npmConfigHook`**: An alternative config hook (from `importNpmLock`) that instead of a single FOD hash, fetches each dependency individually using the integrity hashes already in `package-lock.json`. No separate `npmDepsHash` needed. Used for `importNpmLock`-based workflows.

### `importNpmLock`

`importNpmLock` is an alternative to the `npmDeps` FOD approach for `buildNpmPackage`. Instead of one hash over the entire npm cache:

```nix
buildNpmPackage {
  pname = "my-app";
  version = "1.0.0";
  src = ./.;
  npmDeps = importNpmLock {
    npmRoot = ./.;
    # No hash needed — uses integrity fields from package-lock.json
  };
  nativeBuildInputs = [ importNpmLock.npmConfigHook ];
}
```

How it works: `importNpmLock` reads `package-lock.json`, extracts each `integrity` field (which is already an SRI hash), and creates individual FODs for each package. No single `npmDepsHash` to maintain — only `package-lock.json` changes are needed. Git dependencies use `fetchGit`.

Limitation: only works with `lockfileVersion: 2` or `3` (npm 7+). `lockfileVersion: 1` (npm 6) lacks per-package integrity hashes for all entries.

### `mkYarnPackage` / `yarn2nix-moretea`

For Yarn 1.x projects, `pkgs.mkYarnPackage` (from `yarn2nix-moretea`, included in nixpkgs) provides offline Yarn builds:

```nix
pkgs.mkYarnPackage {
  name = "my-app";
  src = ./.;
  packageJSON = ./package.json;
  yarnLock = ./yarn.lock;
  # Optionally provide a pre-generated yarnNix file:
  # yarnNix = ./yarn.nix;  # generated by: yarn2nix > yarn.nix
}
```

Workflow:
1. Commit `yarn.lock` to the repository.
2. Either: run `yarn2nix > yarn.nix` and commit `yarn.nix` (explicit), or let `mkYarnPackage` generate it via IFD (implicit, not allowed in nixpkgs).
3. `mkYarnPackage` fetches each dependency as an individual FOD using the hashes from `yarn.lock`.
4. Builds with `yarn install --offline` against the pre-fetched store.

`mkYarnModules` is a lower-level variant that produces only the `node_modules/` directory without wrapping it into a full package. Useful when you need to compose manually.

**Current status**: `mkYarnPackage` is maintained but has fallen out of favor in nixpkgs for new packages. Yarn v2/v3/v4 (Plug'n'Play) is not well supported by `mkYarnPackage`. For Yarn v2+, the recommended approach is to use `buildNpmPackage` with `yarn berry` patched to use npm semantics, or to use `pnpm`.

### `node2nix`: The Code-Generation Approach

`node2nix` (by Sander van der Burg) generates Nix expressions from `package.json` rather than wrapping the npm CLI:

```bash
# Generate Nix expressions:
node2nix -i node-packages.json -o node-packages.nix -c composition.nix
# node-packages.json: list of package names/versions to expose
```

Generated files:
- `node-packages.nix`: Every package + its full transitive dependency tree as Nix expressions, with per-package SRI hashes.
- `composition.nix`: Wires the package set together with `nodeEnv`.
- `node-env.nix`: The `buildNodePackage` function that `node-packages.nix` uses.

Historical use: nixpkgs's `pkgs/development/node-packages/` uses this approach for global CLI tools (`nodePackages.typescript`, `nodePackages.prettier`, etc.). As of early 2024, `pkgs/development/node-packages/node-packages.json` lists ~408 packages. Running the generation script takes ~4 hours.

**Current status in nixpkgs**: Effectively deprecated for new additions. `buildNpmPackage` is now required for new Node.js packages in nixpkgs. Existing `node-packages/` entries are maintained but not growing. The maintainability problem: every addition or update requires regenerating `node-packages.nix` for ALL 408 packages because the dependency graph is fully resolved at generation time.

### `pnpm` Support: `fetchPnpmDeps` + `pnpmConfigHook`

Added to nixpkgs with `pnpm` v9 support (nixpkgs ≥ 24.05):

```nix
{ lib, buildNpmPackage, fetchFromGitHub, fetchPnpmDeps, pnpm_9, nodejs }:

buildNpmPackage {
  pname = "my-pnpm-app";
  version = "1.0.0";
  src = fetchFromGitHub { /* ... */ };

  npmDeps = fetchPnpmDeps {
    inherit (finalAttrs) pname version src;
    fetcherVersion = 3;  # matches pnpm-lock.yaml lockfileVersion
    hash = "sha256-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx=";
  };

  nativeBuildInputs = [ pnpm_9.configHook nodejs ];

  buildPhase = ''
    pnpm --filter=my-app build
  '';
}
```

Key points:
- `fetchPnpmDeps` is a FOD that produces a pnpm virtual store.
- `fetcherVersion` must match the `lockfileVersion` in `pnpm-lock.yaml`. pnpm 8 uses `lockfileVersion: "6.0"`; pnpm 9 uses `"9.0"`.
- `pnpm_9.configHook` (also accessible as `pnpmConfigHook`) sets `PNPM_HOME` and configures pnpm to use the pre-fetched store.
- Pin pnpm version explicitly: use `pnpm_9` or `pnpm_10` rather than bare `pnpm` to ensure reproducibility as the default version advances.
- For pnpm workspace projects, `npmHooks.npmBuildHook` typically won't work — use `pnpm --filter=<workspace-name> build` manually.

To update the hash: set `hash = lib.fakeHash;`, build, read correct hash from error.

---

## Topic 4: Go (`buildGoModule` Non-Obvious Details)

### `CGO_ENABLED` Default and Static Binaries

`buildGoModule` sets `CGO_ENABLED` via `env.CGO_ENABLED`, which defaults to the Go package's own default: **`1` (CGO enabled)**. The Go toolchain enables CGO by default on platforms where a C compiler is available (all Linux/macOS in nixpkgs).

To produce a pure-Go static binary:

```nix
buildGoModule {
  pname = "myapp";
  # ...
  env.CGO_ENABLED = "0";
  # ldflags for extra static linking assurance:
  ldflags = [ "-s" "-w" "-extldflags=-static" ];
}
```

With `CGO_ENABLED = "0"`: no C compiler in `buildInputs` is needed, and the binary links only the Go runtime. With `CGO_ENABLED = "1"` (default), you must add any required C libraries to `buildInputs` and the binary will dynamically link against them unless you also use `pkgsStatic`.

For fully-musl-static binaries: use `pkgs.pkgsStatic.buildGoModule { ... }` which replaces glibc with musl and enables fully static linking without needing to set `CGO_ENABLED = "0"` manually (though setting it still helps for clarity).

### `subPackages`: Building Specific Commands

```nix
buildGoModule {
  pname = "kubernetes";
  version = "1.29.0";
  src = /* ... */;
  vendorHash = "sha256-...";

  # Only build kubectl and kubelet, not the entire k8s monorepo:
  subPackages = [
    "cmd/kubectl"
    "cmd/kubelet"
  ];
}
```

`subPackages` is a string or list of strings specifying subdirectories relative to the source root. The builder runs `go build ./cmd/kubectl ./cmd/kubelet` instead of `go build ./...`. Without it, `buildGoModule` builds all `main` packages found anywhere in the module, which for large monorepos is very slow.

Complementary attribute: `excludedPackages` — a list of patterns to skip from the default `./...` glob (useful when most packages should build but a few problematic ones should be skipped).

### `nativeBuildInputs` for Code Generation

Any build-time code generators must be in `nativeBuildInputs` so they're on `$PATH` during the build phase:

```nix
buildGoModule {
  pname = "myservice";
  # ...
  nativeBuildInputs = [
    pkgs.protoc-gen-go        # generates .pb.go from .proto
    pkgs.protoc-gen-go-grpc   # generates _grpc.pb.go
    pkgs.protobuf              # provides protoc
    pkgs.buf                   # protobuf build tool
  ];
  # Generation runs in preBuild:
  preBuild = ''
    buf generate
  '';
}
```

These go in `nativeBuildInputs` (not `buildInputs`) because they are host executables needed during build, not runtime deps of the output binary.

### `gomod2nix`: Eliminating `vendorHash` Churn

`gomod2nix` (nix-community) is a code-generation approach that eliminates the `vendorHash` FOD entirely:

**Workflow:**
```bash
# 1. Add gomod2nix to your devShell:
nativeBuildInputs = [ gomod2nix ];

# 2. Generate the lock file (run after any go.mod/go.sum change):
gomod2nix generate  # creates gomod2nix.toml

# 3. Commit gomod2nix.toml alongside go.mod/go.sum
```

**Package definition using `buildGoApplication`:**
```nix
{ buildGoApplication }:

buildGoApplication {
  pname = "myapp";
  version = "0.1.0";
  src = ./.;
  modules = ./gomod2nix.toml;  # replaces vendorHash
}
```

`gomod2nix.toml` contains each Go module dependency with its path, version, and hash — essentially a per-module lock file analogous to `cargoLock`. Unlike `vendorHash` (which hashes the entire vendor directory as one FOD), `gomod2nix` creates individual FODs per module and pre-compiles them.

**Key differences from `buildGoModule`:**
- No `vendorHash` to recompute on every `go.mod` change — only `gomod2nix.toml` changes, and only for the specific module that changed.
- Pre-compiled dependencies reduce subsequent build times.
- `CGO_ENABLED` defaults to the Go package default (same as `buildGoModule`).
- `subPackages` works the same way.
- The `modules` attribute defaults to `./gomod2nix.toml` in the current directory.

**Flake integration:**
```nix
{
  inputs.gomod2nix.url = "github:nix-community/gomod2nix";
  outputs = { nixpkgs, gomod2nix, ... }: {
    packages.x86_64-linux.default = gomod2nix.legacyPackages.x86_64-linux.buildGoApplication {
      pname = "myapp"; version = "0.1.0"; src = ./.; modules = ./gomod2nix.toml;
    };
    devShells.x86_64-linux.default = nixpkgs.legacyPackages.x86_64-linux.mkShell {
      packages = [ gomod2nix.packages.x86_64-linux.default ];
    };
  };
}
```

**Caveat**: `gomod2nix` is a community tool, not in nixpkgs core. For packages intended for nixpkgs contribution, `buildGoModule` with `vendorHash` remains the standard. `gomod2nix` is primarily valuable for private/flake-based projects where the developer controls the tooling.

### Other Non-Obvious `buildGoModule` Attributes

```nix
buildGoModule {
  # Reproducibility: -trimpath removes build-host paths from binaries (default: added automatically)
  # Version injection via ldflags:
  ldflags = [ "-s" "-w" "-X main.version=${version}" "-X main.commit=${src.rev}" ];

  # Build constraint tags:
  tags = [ "sqlite" "production" ];  # passed as -tags sqlite,production

  # Use pre-vendored sources in repo (skip the FOD fetch):
  vendorHash = null;

  # Delete an existing vendor/ dir and re-vendor (handle stale vendor/):
  deleteVendor = true;

  # Use go mod download instead of go mod vendor (for C-code deps with case-sensitive issues):
  proxyVendor = true;

  # Allow the Go toolchain itself to be referenced in the output (default: false → adds -trimpath):
  allowGoReference = false;

  # Build only test binaries (for testing utilities):
  buildTestBinaries = true;

  # Root directory containing go.mod (for monorepos):
  modRoot = "./services/myservice";
}
```

---

## Topic 5: Nixpkgs Branch Structure

### The Branch Hierarchy

Nixpkgs maintains parallel branches at different stability levels. Understanding which branch to target is essential for contributions:

```
master
  ↑ (merged from staging-next periodically)
staging-next
  ↑ (merged from staging periodically)
staging
```

**`master`**: The primary development branch. All non-mass-rebuild changes targeting `nixos-unstable` and `nixpkgs-unstable` land here. PRs that affect <500 packages are appropriate for `master`.

**`staging-next`**: An intermediate branch built by Hydra. Changes from `staging` are manually batch-merged into `staging-next` (typically twice per release cycle), Hydra builds the result, and once it's green, `staging-next` merges into `master`. This is the Hydra-visible gate for staging changes.

**`staging`**: The collection point for mass-rebuilds. PRs targeting `staging` are not built by Hydra directly — they wait until a manual `staging-next` iteration is triggered. Use `staging` when your change rebuilds 500+ packages (e.g., updating a widely-depended-on library like `glibc`, `openssl`, or `python3`).

**Release branches** (`release-24.11`, `release-25.05`, etc.): Stable release branches. Only security patches and critical bug fixes are backported here. Maintainers use the `backport release-YY.MM` label to trigger automated backport PRs (see below). Corresponding `staging-24.11` and `staging-next-24.11` branches exist for mass-rebuild changes to stable.

### Channel Advancement: `nixos-unstable-small` vs `nixos-unstable`

Channels are git refs that Hydra advances as different subsets of packages pass:

| Channel | Hydra Jobset | Packages Built Before Advancing | Update Frequency |
|---------|-------------|--------------------------------|-----------------|
| `nixos-unstable-small` | `nixos/unstable-small/tested` | Curated critical subset (~hundreds of packages) | Every few hours |
| `nixos-unstable` | `nixos/trunk-combined/tested` | Full nixpkgs breadth (~80k+ packages) | Every few days |
| `nixpkgs-unstable` | `nixpkgs/trunk/unstable` | Critical non-NixOS packages | Similar to unstable-small |
| `nixos-24.11-small` | `nixos/nixos-24.11-small/tested` | Small subset for stable release | Frequent |
| `nixos-24.11` | `nixos/nixos-24.11/tested` | Full stable breadth | Slower |

**`nixos-unstable-small` vs `nixos-unstable`**: Both track `master`. `small` advances as soon as its curated subset builds; `unstable` waits for the entire breadth. If you need a quick security fix available via binary cache, `nixos-unstable-small` receives it sooner. The trade-off: packages not in the curated subset may require local builds on `nixos-unstable-small`.

**`nixpkgs-unstable` vs `nixos-unstable`**: `nixpkgs-unstable` does not run NixOS integration tests (it only runs the `nixpkgs/trunk/unstable` job, not `trunk-combined/tested`). It's for users on non-NixOS (macOS, other Linux distros) who want new packages without waiting for NixOS module tests to pass.

---

## Topic 6: ofborg and GitHub Actions CI

### ofborg: Historical Overview

**ofborg** was the primary PR CI bot for nixpkgs from ~2017 to 2024. It:

1. **Evaluated** PRs: ran `nix-instantiate` on the entire nixpkgs with the PR applied to check for eval errors. Results posted as PR checks.
2. **Built packages**: built changed packages on Linux and Darwin, posting pass/fail per package.
3. **Applied labels**: computed which packages would be rebuilt and applied labels like `10.rebuild-linux: 1-10` to PRs automatically.
4. **Checked maintainers**: verified that any new/changed package's maintainer was listed in `maintainers/maintainer-list.nix`.

**Triggering ofborg manually (historical)**: PR comments starting with `@ofborg`:
- `@ofborg eval` — trigger evaluation
- `@ofborg build <attr>` — build a specific attribute
- `@ofborg test <nixosTest>` — run a NixOS test

**Hydra vs ofborg**: Hydra is nixpkgs's main build farm — it builds the entire package set and produces the binary cache that powers `nixos-unstable`, `nixos-24.11`, etc. Hydra does not look at PRs; it builds branches. ofborg was PR-specific: lightweight checks (eval + changed packages) that gave contributors fast feedback before merge, without waiting for Hydra.

### ofborg Deprecation (Late 2024–2025)

In November 2024, Equinix Metal ended its sponsorship of the infrastructure hosting ofborg. The nixpkgs team migrated ofborg's functions to **GitHub Actions workflows**:

- GitHub Actions now handles eval checks (running in 5–15 minutes, faster than ofborg's queue).
- Package rebuild counting and label application migrated to GitHub Actions.
- Maintainer checks migrated to GitHub Actions.
- The `Replacing ofborg with GitHub Actions` tracking issue (#355847) was closed in June 2025 with the core functions migrated.

Functions not yet fully replaced as of mid-2025: optional on-demand package building for Linux/macOS (ofborg's `@ofborg build` command), and NixOS option evaluation. These are lower priority since the nixpkgs-review tool covers the build-on-demand use case.

**Current state (2026)**: GitHub Actions is the required CI for nixpkgs merges. ofborg is effectively retired. nixpkgs-review 3.0 added support for using GitHub Actions evaluation results directly, closing the loop.

### PR Labels

Labels are applied automatically by GitHub Actions (previously by ofborg) based on the number of packages rebuilt:

| Label | Meaning | Action Required |
|-------|---------|----------------|
| `10.rebuild-linux: 1` | Rebuilds 1 Linux package | Fine for `master` |
| `10.rebuild-linux: 1-10` | Rebuilds 1–10 Linux packages | Fine for `master` |
| `10.rebuild-linux: 11-100` | Rebuilds 11–100 Linux packages | Fine for `master` |
| `10.rebuild-linux: 101-500` | Rebuilds 101–500 Linux packages | Consider `staging` |
| `10.rebuild-linux: 501-1000` | Rebuilds 501–1000 Linux packages | Should target `staging` |
| `10.rebuild-linux: 1001+` | Mass rebuild (>1000 packages) | **Must** target `staging` |
| `10.rebuild-darwin: ...` | Same scale for Darwin/macOS | Same rules |
| `10.rebuild-nixos-tests: ...` | NixOS integration tests affected | Target `staging-nixos` |
| `6.topic: staging` | PR should target `staging` branch | Retarget the PR |
| `backport release-24.11` | Triggers automated backport PR | Applied by maintainers |
| `2.status: merge-ready` | Passes all required checks | Bot-applied when CI passes |

Human-applied labels include `6.topic: staging` (when maintainers decide a PR belongs there) and backport labels. The rest are bot-applied.

---

## Topic 7: `nixpkgs-review`: Reviewing PRs Locally

### What It Does

`nixpkgs-review` (by Mic92) automates the full PR review build cycle. Running `nixpkgs-review pr 123456` does:

1. Fetches PR metadata from GitHub API.
2. Creates a `git worktree` — your main checkout is untouched.
3. Merges the PR branch into the target branch in the worktree.
4. Identifies changed packages by using ofborg's evaluation result (if available) or running local evaluation (`nix-instantiate`).
5. Builds all changed packages using the Nix binary cache — packages already in the cache are not rebuilt.
6. Drops you into a shell containing all successfully built packages for manual testing.
7. Generates a structured report listing `built`/`failed`/`broken`/`blacklisted` per package.

### Key Flags

```bash
# Basic usage:
nixpkgs-review pr 123456

# Non-interactive (for scripts/automation):
nixpkgs-review pr 123456 --no-shell

# Post build report as a GitHub PR comment:
nixpkgs-review pr 123456 --post-result

# Run a command in the shell and exit (instead of interactive):
nixpkgs-review pr 123456 --run "rg --version"

# Force local evaluation instead of ofborg results:
nixpkgs-review pr 123456 --eval local

# Build for multiple systems:
nixpkgs-review pr 123456 --systems linux  # or darwin, all

# Filter packages by regex:
nixpkgs-review pr 123456 --package-regex 'python.*'
nixpkgs-review pr 123456 --skip-package-regex '.*tests.*'

# Print report to terminal (vs --post-result for GitHub):
nixpkgs-review pr 123456 --print-result
```

**nixpkgs-review 3.0** (released early 2025): Added support for using GitHub Actions evaluation results directly, replacing the dependency on ofborg's eval API. This means nixpkgs-review works correctly even with ofborg retired.

### Contrast with Manual `nix build`

| Approach | Effort | Isolation | Package Discovery | Cache Use |
|----------|--------|-----------|-------------------|-----------|
| `nix build nixpkgs#foo` | Low | None (modifies your nixpkgs checkout) | Manual | Yes |
| `nixpkgs-review pr N` | One command | Worktree (safe) | Automatic (all changed packages) | Yes |

The key advantage of `nixpkgs-review`: you don't need to manually identify which packages changed. For PRs that bump a core library (say, `openssl`), hundreds of packages might be affected — `nixpkgs-review` finds and builds all of them, leveraging the binary cache for anything already built.

### Batch Review Workflow

```bash
# Review multiple PRs unattended, post results to GitHub:
for pr in 80760 80761 80762; do
  nixpkgs-review pr --no-shell --post-result "$pr"
done
```

GitHub token must have `public_repo` scope for `--post-result`. Automatically uses `gh auth token` if the `gh` CLI is installed.

---

## Topic 8: `nix-update`: Automating Package Version Bumps

### What It Does

`nix-update` (by Mic92) automates the mechanical parts of updating a nixpkgs package: detecting the latest version from upstream, updating the `version` string in the Nix file, and recomputing all hashes (`src.hash`, `vendorHash`, `cargoHash`, etc.):

```bash
# Basic: update package to latest release
nix-update hello

# Specific version:
nix-update hello --version 2.12.1

# Track latest commit on default branch (for rolling packages):
nix-update hello --version=branch

# Track a specific branch:
nix-update hello --version=branch=develop

# For packages defined in a flake (not in nixpkgs):
nix-update --flake myapp

# Build after update to verify it works:
nix-update hello --build

# Open a nix-shell with the updated package:
nix-update hello --shell

# Create a git commit with the update:
nix-update hello --commit

# Also run nixpkgs-review on the result:
nix-update hello --review
```

### What Files It Modifies

`nix-update` locates the Nix file by evaluating `(import <nixpkgs> {}).hello.meta.position` to find the source file and line. It then:

1. Updates the `version = "..."` string.
2. Sets `hash = lib.fakeHash;` (or the appropriate hash attr), builds to get the correct hash, sets it.
3. For Rust packages: also updates `cargoHash`.
4. For Go packages: also updates `vendorHash`.
5. For npm packages: also updates `npmDepsHash`.
6. For Python packages with `fetchPypi`: derives the new URL from `version` and updates `hash`.

### `nix-update-script` in `passthru`

Packages that want automated updates (via the `r-ryantm` bot or CI) set:

```nix
passthru.updateScript = nix-update-script { };
# or with flags:
passthru.updateScript = nix-update-script {
  extraArgs = [ "--version=branch" ];
};
```

`nix-update-script` is a nixpkgs helper that wraps `nix-update` with the correct package name. The `r-ryantm` bot runs `maintainers/scripts/update.nix` nightly, which calls `pkg.passthru.updateScript` for every package that has one. PRs are created automatically when new versions are found.

Manual invocation (within nixpkgs checkout):
```bash
# Via maintainer script:
nix-shell maintainers/scripts/update.nix --argstr package hello

# Directly:
nix-update hello
```

The `nix-update-script` approach in `passthru.updateScript` differs from the `gitUpdater` helper (for packages that track git tags directly) and custom shell scripts in `passthru.updateScript` — all three approaches are valid, but `nix-update-script` is the most common.

---

## Topic 9: `pkgs/by-name` Migration

### Structure and Purpose

RFC 140 ("Simple Package Paths", by infinisil/Silvan Mosberger) introduced `pkgs/by-name/` as the standard location for new top-level packages. The structure:

```
pkgs/by-name/XX/package-name/package.nix
```

Where `XX` = `lib.toLower (lib.substring 0 2 "package-name")` — the lowercased first two letters.

Examples:
- `pkgs.ripgrep` → `pkgs/by-name/ri/ripgrep/package.nix`
- `pkgs.hello` → `pkgs/by-name/he/hello/package.nix`
- `pkgs._1password` → `pkgs/by-name/_1/_1password/package.nix`

`package.nix` contains a function receiving `pkgs` top-level attrs as arguments:

```nix
{ lib, stdenv, fetchFromGitHub, cmake, pkg-config, openssl }:

stdenv.mkDerivation (finalAttrs: {
  pname = "ripgrep";
  version = "14.1.1";
  # ...
})
```

Additional files (patches, helper nix files) can live in the same directory.

### What Is and Isn't Allowed

**Allowed in `pkgs/by-name`:**
- Top-level derivations using `callPackage`.
- Packages that are single derivations (no complex overrides needed).

**Not allowed:**
- Packages from language-specific sets (`python3Packages.*`, `perlPackages.*`, etc.) — these must be in their respective package sets.
- Non-derivations (`fetchFromGitHub`, `lib.makeOverridable` wrappers, etc.).
- Packages using `python3Packages.callPackage` or `ruby.withPackages` — these need interpreter-specific callPackage forms.

### Enforcement and `nixpkgs-vet`

`nixpkgs-vet` is the CI tool that enforces `pkgs/by-name` structure. It runs in GitHub Actions on every PR and checks:

1. New top-level packages using `callPackage` must be in `pkgs/by-name/`, not `pkgs/top-level/all-packages.nix`.
2. Package names match their directory names.
3. The shard prefix is correct.
4. No empty/stub `package.nix` files.

This enforcement was enabled progressively. The nixpkgs-merge-bot can auto-merge simple `pkgs/by-name` additions that pass `nixpkgs-vet` without a human reviewer.

### Migration Status

As of early 2026, the migration is ~97% complete by issue milestone. The remaining work:

- Packages with override forms (`callPackage ./foo.nix { foo = bar; }`) in `all-packages.nix` — harder to automate.
- Packages defined inline in `all-packages.nix` rather than in separate files.

`pkgs/top-level/all-packages.nix` is not being deleted — it will continue to hold overrides, package set compositions, and the remaining non-migratable entries. The migration effort is about ensuring that the simple case (a derivation callable with `callPackage`) moves to the discoverable `pkgs/by-name/` structure.

**Automated migration**: `nixpkgs-vet` has a `--migrate` mode that can generate migration PRs automatically, but most migration has been done via manual community contribution rather than a fully automated bot.

---

## Cross-Cutting Patterns

### Finalizing Attributes Pattern (`finalAttrs`)

Many `by-name` packages now use the `finalAttrs` pattern for self-referential expressions:

```nix
stdenv.mkDerivation (finalAttrs: {
  pname = "myapp";
  version = "1.0.0";
  src = fetchFromGitHub {
    owner = "user"; repo = "myapp";
    rev = "v${finalAttrs.version}";  # reference version without infinite recursion
    hash = "sha256-...";
  };
  passthru.tests.version = testers.testVersion { package = finalAttrs.finalPackage; };
})
```

`finalAttrs.finalPackage` is the fully-overridden derivation, enabling `passthru.tests` to reference the package itself correctly even after `overrideAttrs`.

### The `r-ryantm` Bot

`r-ryantm` is a GitHub bot that runs `maintainers/scripts/update.nix` nightly and creates PRs for packages with `passthru.updateScript`. It is distinct from ofborg/GitHub Actions — it is a version-update bot, not a CI bot. Packages that set `passthru.updateScript = nix-update-script {}` get automatic PR creation on new upstream releases.
