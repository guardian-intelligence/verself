# nix search / nix-index, builtins.fetchClosure, Python Environments, and Formatter Ecosystem

Four topics that commonly arise in practical Nix usage but lack consolidated documentation: how package discovery actually works under the hood, the underused `fetchClosure` builtin for trustless binary imports, the full Python environment story from `withPackages` through uv2nix, and the Nix formatter landscape as of 2025-2026.

---

## Part 1: `nix search` and Package Discovery

### 1.1 How `nix search` Works Internally

`nix search <installable> <regex>` evaluates the installable's flake outputs and matches packages against the regex. For nixpkgs, the installable is almost always `nixpkgs` (from the registry) or `github:NixOS/nixpkgs/nixos-unstable`:

```bash
nix search nixpkgs ffmpeg
nix search nixpkgs '^python3[0-9]'  # all Python 3.x
nix search nixpkgs ^                 # all packages (Nix ≥ 2.20 requires explicit ^)
```

**What gets searched:** The command looks in two places:

1. `packages.<system>.<attr>` — all derivations directly under `packages`
2. `legacyPackages.<system>.<attr>` — attribute sets that have `recurseForDerivations = true` set on them are recursed into; attribute sets without it are skipped

For nixpkgs, almost all packages live under `legacyPackages.<system>` because nixpkgs has nested package sets (e.g., `python311Packages.*`, `nodePackages.*`) whose top-level names are attribute sets with `recurseForDerivations = true`. The recursion is deep but gated by that marker — nixpkgs uses it intentionally to expose sub-packages to `nix search`.

**What is displayed:** The full attribute path from the installable root, the `version` attribute of the derivation, and `meta.description`. Matched substrings are highlighted. Output format:

```
* legacyPackages.x86_64-linux.ffmpeg (6.1.1)
  A complete, cross-platform solution to record, convert and stream audio and video
```

**Nix ≥ 2.20 breaking change:** `nix search` without a regex argument is now an error. Use `^` to match all packages: `nix search nixpkgs ^`.

### 1.2 Evaluation Cache Behavior

`nix search` uses the same eval-cache-v6 infrastructure as all other Nix evaluation. The cache lives at `~/.cache/nix/eval-cache-v6/<hash>.sqlite`.

**Cold cache (first run):** Nix must evaluate the entire `legacyPackages.${system}` attribute set of nixpkgs. This evaluation touches roughly 100,000 package definitions and takes **30–90 seconds** on a modern machine. The cache records the result so subsequent searches are instant.

**Cache invalidation:** The cache key includes the flake's `narHash` from `flake.lock`. When you run `nix flake update` and nixpkgs advances to a new commit, the narHash changes and the cache is cold again for one evaluation.

**`--offline` flag:** Disables substituters but does not prevent evaluation. If the eval cache is warm, `nix search --offline nixpkgs term` still works instantly. If the eval cache is cold and nixpkgs hasn't been fetched yet, `--offline` causes failure.

**Local flakes and the eval cache:** The eval cache is disabled for `path:`-type flakes (e.g., `nix search . term`). Every invocation re-evaluates. This is the same behavior as `nix develop .` — the eval cache is only used for flakes with a locked, content-addressed `narHash`.

### 1.3 JSON Output

`nix search --json nixpkgs ffmpeg` produces:

```json
{
  "legacyPackages.x86_64-linux.ffmpeg": {
    "pname": "ffmpeg",
    "version": "6.1.1",
    "description": "A complete, cross-platform solution to record, convert and stream audio and video"
  },
  "legacyPackages.x86_64-linux.ffmpeg-full": {
    "pname": "ffmpeg-full",
    "version": "6.1.1",
    "description": "A complete, cross-platform solution to record, convert and stream audio and video"
  }
}
```

The key is the full attribute path; the value is `{ pname, version, description }`. Note that `description` is the `meta.description` field from the derivation — packages without `meta.description` appear without it.

**Scripting pattern:**

```bash
# Get the version of a specific package
nix search --json nixpkgs '^ffmpeg$' | jq -r '.["legacyPackages.x86_64-linux.ffmpeg"].version'

# List all Python 3.11 packages
nix search --json nixpkgs 'python311Packages' | jq -r 'keys[]'
```

### 1.4 Exclude Flag

`--exclude` / `-e` takes a regex and hides matching results:

```bash
# Find ffmpeg packages but not ffmpeg-full
nix search nixpkgs ffmpeg -e full

# Find Python packages but exclude test packages
nix search nixpkgs python311Packages -e test
```

Multiple `--exclude` flags are OR-combined.

### 1.5 `nix-index` and `nix-locate`

`nix search` finds packages by name/description. `nix-index` solves a different problem: **given a file path (e.g., `bin/ffmpeg`), which nixpkgs package provides it?**

**`nix-index`** (https://github.com/nix-community/nix-index): builds a local database of all files in all nixpkgs packages by querying binary cache NAR indices. Does not build packages locally — it fetches `.narinfo` files to enumerate files.

```bash
# Build the database (takes 5–20 minutes, indexes ~100k packages)
nix-index

# Find which package provides bin/ffmpeg
nix-locate --whole-name bin/ffmpeg
# Output:
# ffmpeg-full.out  ...  /nix/store/...-ffmpeg-full-6.1.1/bin/ffmpeg
# ffmpeg.out       ...  /nix/store/...-ffmpeg-6.1.1/bin/ffmpeg

# Find all packages providing files matching a path fragment
nix-locate libssl.so
# Returns all packages containing a file with libssl.so in the path

# Regex search
nix-locate --regex 'bin/python3\.[0-9]+'
```

**Database location:** `~/.cache/nix-index/files` (a single binary file, typically 200–400 MB for nixos-unstable).

**Flags:**

| Flag | Description |
|------|-------------|
| `--whole-name` | Match the full filename component only (not path fragments) |
| `--regex` | Treat the argument as a regex pattern |
| `--type r` | Filter by file type: `r` (regular), `x` (executable), `s` (symlink), `d` (directory) |
| `-1` | Show only the first result per store path |
| `--db <path>` | Use a specific database file |

**`command-not-found` integration:** `nix-index` ships shell hook scripts (`command-not-found.sh` for bash/zsh, `command-not-found.nu` for Nushell). When a command isn't found, instead of "command not found", the shell suggests which package provides it:

```
$ cowsay hello
nix-shell -p cowsay    # suggested automatically
```

NixOS enables this via `programs.nix-index.enable = true` (which sets `programs.command-not-found.enable = false` to prevent conflict).

### 1.6 `nix-index-database`: Pre-Built Index

Running `nix-index` locally takes 5–20 minutes and requires network access. **`nix-index-database`** (https://github.com/nix-community/nix-index-database) provides a pre-built database updated weekly from nixos-unstable CI.

**Flake integration:**

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    nix-index-database.url = "github:nix-community/nix-index-database";
    nix-index-database.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { nixpkgs, nix-index-database, ... }: {
    nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
      modules = [
        nix-index-database.nixosModules.default
        {
          # Enables nix-index with the pre-built database
          # Also replaces command-not-found with nix-locate suggestions
          programs.nix-index-database.comma.enable = true;  # optional: enable comma
        }
      ];
    };
  };
}
```

**Module names:**

| Platform | Module |
|----------|--------|
| NixOS | `nix-index-database.nixosModules.default` |
| Home Manager | `nix-index-database.homeModules.default` |
| nix-darwin | `nix-index-database.darwinModules.nix-index` |

**Database freshness:** Tagged weekly as `YYYY-MM-DD-HHMMSS`. The flake lock pins to a specific weekly snapshot; run `nix flake update nix-index-database` to advance to the latest.

### 1.7 `comma` (`,`): Run Anything Without Installing

`comma` (https://github.com/nix-community/comma — package name `pkgs.comma`) is a wrapper that finds a package providing the command you typed and runs it via `nix shell` without permanently installing anything:

```bash
, ffmpeg --help      # runs ffmpeg from nixpkgs without installing it
, cowsay hello       # runs cowsay
, python3 -c "import numpy"  # runs python3 with numpy if numpy is found
```

**How it works:**

1. Looks up the program name in the nix-index database using `nix-locate --whole-name --type x bin/<name>`
2. If multiple packages provide it, presents a `fzf` selection menu
3. Runs `nix shell nixpkgs#<attr> -c <name> [args...]`

**Caching levels** (controlled by `COMMA_CACHING`):
- `0`: No caching — always re-runs nix-locate and nix shell
- `1`: Cache the package choice only — re-uses the selected package but still evaluates the shell
- `2` (default): Cache both the choice and the resolved store path — dramatically faster; `nix shell` is avoided entirely once the path is cached

**Level 2 caveat:** A cached path may become stale after `nix-collect-garbage`. If a cached program stops working, run `nix-store --verify` or clear the comma cache at `~/.cache/comma/`.

**Dependency:** comma requires either a local nix-index database (run `nix-index`) or the `nix-index-database` module with `programs.nix-index-database.comma.enable = true`.

### 1.8 `nix-search-cli`: API-Based Search

`nix-search-cli` (https://github.com/peterldowns/nix-search-cli) queries the **search.nixos.org ElasticSearch API** directly, bypassing local evaluation entirely:

```bash
nix-search ffmpeg           # search by name/description
nix-search --program gcloud  # find package providing a binary
nix-search --version '1.*' ripgrep  # filter by version
nix-search --channel stable --json ffmpeg  # specific channel, JSON output
```

**Key difference from `nix search`:** Does not evaluate Nix code locally. Works offline only if caching is available. Results reflect the search.nixos.org index which lags the nixpkgs master branch by some hours.

**Use case:** Quick lookups on systems where nixpkgs isn't cached, or in CI where the eval overhead of `nix search` is undesirable.

---

## Part 2: `builtins.fetchClosure`

### 2.1 What It Is

`builtins.fetchClosure` fetches a store path closure from a remote binary cache into the local Nix store **at evaluation time** — not at build time. This is the only way to make a pre-built binary available as a Nix value without writing a derivation.

It was introduced in Nix 2.8 (April 2022) as an experimental feature. As of Nix 2.30 it remains behind the `fetch-closure` experimental feature flag.

**Experimental feature required:**

```
# nix.conf or nix.settings.extra-experimental-features
extra-experimental-features = fetch-closure
```

### 2.2 Signature and Parameters

```nix
builtins.fetchClosure {
  fromStore   # string: URL of binary cache to fetch from (required)
  fromPath    # path: store path to fetch (required)
  toPath      # path: expected content-addressed output path (optional)
  inputAddressed  # bool: allow fetching input-addressed paths as-is (optional, default false)
}
```

Returns: a string with Nix string context pointing to the fetched store path. That context propagates into derivations that reference the returned string, ensuring the path is present at build time.

### 2.3 Three Usage Patterns

The three patterns differ in whether the fetched path is content-addressed (CA) or input-addressed (IA), and whether trust configuration is required.

#### Pattern 1: Fetch a CA Path (No Trust Required)

The path is already content-addressed on the remote cache. CA paths are self-certifying — their hash is computed from their content, so no signature verification against `trusted-public-keys` is needed.

```nix
# Works in pure eval mode, no trusted-public-keys needed
let
  git = builtins.fetchClosure {
    fromStore = "https://cache.nixos.org";
    fromPath = /nix/store/ldbhlwhh39wha58rm61bkiiwm6j7211j-git-2.33.1;
  };
in
  "${git}/bin/git"
```

**How to check if a path is CA:** `nix path-info --json /nix/store/<hash>-<name>` — if `"ca"` field is present, it is CA.

#### Pattern 2: Fetch IA Path and Rewrite to CA Form

The remote cache has an input-addressed path, but you want to fetch it without configuring `trusted-public-keys`. Nix downloads the path and rewrites it to its CA equivalent in the local store. The `toPath` field specifies the expected CA store path.

```nix
builtins.fetchClosure {
  fromStore = "https://cache.nixos.org";
  fromPath  = /nix/store/r2jd6ygnmirm2g803mksqqjm4y39yi6i-git-2.33.1;  # IA path
  toPath    = /nix/store/ldbhlwhh39wha58rm61bkiiwm6j7211j-git-2.33.1;  # CA rewrite
}
```

**Finding `toPath`:** Use `nix store make-content-addressed --from <cache-url> <ia-path>` on a system where the CA feature is enabled. This computes the expected CA path without downloading.

```bash
nix store make-content-addressed --from https://cache.nixos.org \
  /nix/store/r2jd6ygnmirm2g803mksqqjm4y39yi6i-git-2.33.1
# Outputs: /nix/store/ldbhlwhh39wha58rm61bkiiwm6j7211j-git-2.33.1
```

#### Pattern 3: Fetch IA Path As-Is (Requires Trust)

Fetch an input-addressed path without rewriting. This requires the client to trust the binary cache's signatures:

```nix
builtins.fetchClosure {
  fromStore      = "https://cache.nixos.org";
  fromPath       = /nix/store/r2jd6ygnmirm2g803mksqqjm4y39yi6i-git-2.33.1;
  inputAddressed = true;
}
```

**Trust requirement:** The fetching Nix daemon must have the binary cache's public key in `trusted-public-keys`, OR the cache must be listed in `trusted-substituters` (for non-root users), OR `require-sigs = false` must be set (dangerous: disables all signature verification).

**This pattern requires `--impure` in pure eval mode** (pre-Nix 2.17). As of Nix 2.17, `fetchClosure` with `inputAddressed = true` works in pure eval mode if the path is trusted.

### 2.4 Comparison to Related Builtins

| Builtin | Requires path to exist locally? | Downloads from remote? | CA rewriting? | Trust required? |
|---------|--------------------------------|------------------------|---------------|-----------------|
| `builtins.storePath` | Yes | No | No | N/A (path must already be trusted) |
| `builtins.fetchClosure` (CA) | No | Yes | Optional | No |
| `builtins.fetchClosure` (IA) | No | Yes | No | Yes |

`builtins.storePath /nix/store/...` fails at eval time if the path does not exist in the local store. `fetchClosure` downloads from `fromStore` if absent. This is similar to IFD but with explicit store paths rather than derived paths computed from a derivation.

### 2.5 Security Model

**Content-addressed paths are trustless.** The store path hash is computed from the NAR content. If the downloaded content hashes to the expected hash, it is correct regardless of who signed it or what `trusted-public-keys` says. This is the same property as fixed-output derivations.

**Input-addressed paths require trust.** The IA hash is computed from the build inputs (derivation graph), not the outputs. A malicious cache could serve a different binary at the same IA path. This is why IA paths require signature verification.

**`require-sigs = false`** disables all signature checking globally. Do not use in production; it allows any substituter to inject arbitrary store paths.

**`fromStore` can be any URL** accepted by Nix binary cache protocol: `https://`, `file://`, `s3://`, `ssh://`, `daemon://`. The path is verified according to the trust model above regardless of origin.

### 2.6 Use Cases

**Embedding pre-built toolchain artifacts:**

```nix
# In a devShell, pre-fetch a specific known-good binary
devShells.x86_64-linux.default = pkgs.mkShell {
  packages = [
    (builtins.fetchClosure {
      fromStore = "https://cache.nixos.org";
      fromPath  = /nix/store/ldbhlwhh39wha58rm61bkiiwm6j7211j-git-2.33.1;
    })
  ];
};
```

**Fetching a specific closure version in eval-time config:** When a NixOS module needs a specific pre-built path that should not vary with nixpkgs updates, `fetchClosure` with a pinned `fromPath` is more stable than `pkgs.git` (which changes with nixpkgs).

**Cross-cache path sharing:** A self-hosted cache can serve CA paths that clients fetch without needing to add the cache to `trusted-public-keys`.

**Limitation:** `fetchClosure` is IFD-like in that it downloads at eval time. This means it cannot be used in derivations built during `nix build --no-eval` restricted evaluation. It also means eval time includes network latency unless the path is already in the local store.

---

## Part 3: `python3.withPackages` and Python Environments

### 3.1 `python3.withPackages`: How It Works

`python3.withPackages` is the idiomatic way to create a Python interpreter with a specific set of packages available for import:

```nix
python3.withPackages (ps: [ ps.requests ps.numpy ps.boto3 ])
```

This returns a derivation containing a `bin/python3` wrapper. Calling `.withPackages` is equivalent to:

```nix
python3.buildEnv.override {
  extraLibs = [ python3Packages.requests python3Packages.numpy python3Packages.boto3 ];
}
```

**`buildEnv` relationship:** `withPackages` is a convenience wrapper around `python.buildEnv`. The underlying `buildEnv` call creates a merged environment using `pkgs.buildEnv` that combines the interpreter and all package closures under a single store path. This is distinct from a virtualenv — it is a Nix derivation in `/nix/store/`.

**The `pythonModule` filter:** `withPackages` filters the provided packages to ensure they are Python packages (built by `buildPythonPackage` or `buildPythonApplication`). The filter checks for `passthru.pythonModule == python` on each package. This is why you cannot add an arbitrary nixpkgs package to `withPackages` — it must have been built against the same Python interpreter.

**The `env` attribute:** The derivation returned by `withPackages` has an `.env` attribute that creates a development environment suitable for `nix-shell`. This is a backwards-compatible pattern:

```nix
# shell.nix (legacy pattern)
(python3.withPackages (ps: [ ps.numpy ps.requests ])).env
```

**Advanced options via `buildEnv.override`:** When `withPackages` is insufficient, use `buildEnv.override` directly:

```nix
python3.buildEnv.override {
  extraLibs = [ python3Packages.numpy ];
  ignoreCollisions = true;  # allow duplicate files (default: error on collision)
  postBuild = ''
    # custom post-processing of the merged environment
  '';
}
```

### 3.2 `python3.pkgs` vs `python3Packages`

These are the same attribute set accessed two ways:

```nix
# These two are identical:
python3.pkgs.requests
python3Packages.requests

python311.pkgs.numpy
python311Packages.numpy
```

`python3Packages` in the nixpkgs top-level is an alias for `python3.pkgs`. Each Python interpreter version (`python39`, `python311`, `python312`, `python313`) has its own `.pkgs` attribute set, generated from the same `pkgs/top-level/python-packages.nix` fixpoint but scoped to that interpreter.

### 3.3 `withPackages` in devShells

**Pattern 1: withPackages as the packages entry**

```nix
devShells.x86_64-linux.default = pkgs.mkShell {
  packages = [
    (pkgs.python311.withPackages (ps: [
      ps.numpy
      ps.requests
      ps.pytest
      ps.mypy
    ]))
    pkgs.ruff        # linter (not a Python package, goes here not in withPackages)
    pkgs.black       # formatter
  ];
};
```

Advantage: all Python deps are on `PYTHONPATH` for the interpreter. The interpreter and all packages are in a single derivation.

**Pattern 2: mixing python3Packages directly into packages**

```nix
devShells.x86_64-linux.default = pkgs.mkShell {
  packages = [
    pkgs.python3
    pkgs.python3Packages.numpy
    pkgs.python3Packages.requests
  ];
};
```

This works but is fragile: `PYTHONPATH` is set by `buildPythonPackage`'s setup hook, so packages installed this way may or may not be importable depending on hook ordering. **The `withPackages` pattern is more reliable** because it creates a single merged environment with a wrapper script that sets `PYTHONPATH` correctly.

**Pattern 3: `buildInputs` with the withPackages interpreter**

```nix
devShells.x86_64-linux.default = pkgs.mkShell {
  buildInputs = [
    (pkgs.python311.withPackages (ps: [ ps.numpy ps.scipy ]))
  ];
  shellHook = ''
    export PYTHON_ENV="${pkgs.python311.withPackages (ps: [ ps.numpy ])}"
  '';
};
```

### 3.4 Multiple Python Versions

Each Python version is a separate top-level attribute in nixpkgs:

| Attribute | Interpreter | Package set |
|-----------|-------------|-------------|
| `python39` | CPython 3.9.x | `python39Packages` |
| `python310` | CPython 3.10.x | `python310Packages` |
| `python311` | CPython 3.11.x | `python311Packages` |
| `python312` | CPython 3.12.x | `python312Packages` |
| `python313` | CPython 3.13.x | `python313Packages` |
| `python3` | Alias for latest stable (3.12 as of 2025) | `python3Packages` |
| `pypy39` | PyPy 3.9 | `pypy39Packages` |

**Version matrix builds in flakes:**

```nix
let
  pythonVersions = [ "python311" "python312" "python313" ];
  testFor = pyAttr: pkgs.${pyAttr}.withPackages (ps: [ ps.requests ps.pytest ]);
in
  builtins.listToAttrs (map (py: { name = py; value = testFor py; }) pythonVersions)
```

### 3.5 `python3.pkgs.callPackage`: Adding Custom Packages

To add a custom Python package that integrates with the package set (visible to `withPackages`, `buildEnv`, and propagation):

```nix
# In a flake overlay
nixpkgs.overlays = [(final: prev: {
  python3 = prev.python3.override {
    packageOverrides = pyFinal: pyPrev: {
      mylib = pyPrev.buildPythonPackage {
        pname = "mylib";
        version = "0.1.0";
        src = ./src;
        format = "pyproject";
        nativeBuildInputs = [ pyPrev.hatchling ];
        propagatedBuildInputs = [ pyPrev.requests ];
        pythonImportsCheck = [ "mylib" ];
      };
    };
  };
})];

# Usage after overlay is applied:
python3.withPackages (ps: [ ps.mylib ps.requests ])
# mylib is in the package set, ps.requests propagated automatically
```

**Why not `pkgs.callPackage`?** Top-level `callPackage` places the package in the nixpkgs top-level namespace, not in any Python interpreter's package set. It won't be accessible via `python3.pkgs.mylib` and won't propagate correctly into `withPackages` environments.

### 3.6 `poetry2nix`: Status and API

**Current status (2025):** `poetry2nix` is effectively unmaintained. The original author (@adisbladis) recommends `uv2nix` for new projects. The project will not support Poetry 2.0 or PEP-621. Existing projects using it may continue, but new projects should use `uv2nix` or native `buildPythonPackage`.

**Core API (for legacy projects):**

```nix
{
  inputs.poetry2nix.url = "github:nix-community/poetry2nix";

  outputs = { nixpkgs, poetry2nix, ... }:
  let
    pkgs = import nixpkgs { system = "x86_64-linux"; };
    p2n = poetry2nix.lib.mkPoetry2Nix { inherit pkgs; };
  in {
    # Production application (installs scripts from pyproject.toml)
    packages.x86_64-linux.myapp = p2n.mkPoetryApplication {
      projectDir = ./.;
      # overrides for packages with broken metadata:
      overrides = p2n.defaultPoetryOverrides.extend (final: prev: {
        somelib = prev.somelib.overridePythonAttrs (old: {
          nativeBuildInputs = (old.nativeBuildInputs or []) ++ [ pkgs.pkg-config ];
        });
      });
    };

    # Development environment (interpreter + all deps including dev group)
    devShells.x86_64-linux.default = p2n.mkPoetryEnv {
      projectDir = ./.;
      editablePackageSources = { myapp = ./src; };  # editable install
    };
  };
}
```

**`defaultPoetryOverrides`:** A large collection of overrides for packages that have incorrect metadata (missing C build tools, wrong build backends, etc.). Always extend rather than replace: `p2n.defaultPoetryOverrides.extend (final: prev: { ... })`.

**Groups support (Poetry 1.2.0+):**

```nix
p2n.mkPoetryApplication {
  projectDir = ./.;
  groups = [ "main" ];      # Only install main group (default)
  checkGroups = [ "test" ];  # Install test group for checkPhase
}
```

### 3.7 `pyproject.nix`: The Foundation Layer

**`pyproject.nix`** (https://github.com/nix-community/pyproject.nix) is a library of Nix utilities for parsing Python project metadata. It is the foundation that `uv2nix` builds on.

**What it provides:**

- PEP-621 (`pyproject.toml`) parsing
- PEP-440 version specifier evaluation
- PEP-508 dependency marker evaluation (environment markers like `python_version >= "3.11"`)
- Poetry `pyproject.toml` loading
- `requirements.txt` parsing
- Build system detection

**When to use directly:** Rarely. Most users interact with `uv2nix` or (legacy) `poetry2nix`, both of which call `pyproject.nix` internally. You would use `pyproject.nix` directly when building a custom workflow or a tool that needs Python metadata parsing without being tied to a specific lockfile format.

### 3.8 `uv2nix`: The Modern Python Packaging Approach

**`uv2nix`** (https://github.com/adisbladis/uv2nix, docs: https://pyproject-nix.github.io/uv2nix/) converts a `uv` workspace (a `uv.lock`-managed Python project) into Nix derivations using pure Nix code. No `buildPythonPackage` expressions need to be written manually.

**Dependencies:** `uv` for lockfile generation; `uv2nix` for Nix conversion; `pyproject.nix` (dependency of uv2nix).

**Status (2026):** Actively maintained by @adisbladis (same author as poetry2nix's replacement recommendation). The recommended approach for new Python projects in Nix.

**Core workflow:**

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    uv2nix.url = "github:adisbladis/uv2nix";
    uv2nix.inputs.nixpkgs.follows = "nixpkgs";
    pyproject-nix.url = "github:nix-community/pyproject.nix";
    pyproject-nix.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { nixpkgs, uv2nix, pyproject-nix, ... }:
  let
    pkgs = import nixpkgs { system = "x86_64-linux"; };
    python = pkgs.python312;

    # Load the uv workspace from the current directory
    workspace = uv2nix.lib.workspace.loadWorkspace { workspaceRoot = ./.; };

    # Create a package overlay from the workspace
    overlay = workspace.mkPyprojectOverlay {
      sourcePreference = "wheel";  # or "sdist" to build from source
    };

    # Apply the overlay to the Python package set
    pyprojectOverrides = _final: _prev: {
      # Add build system overrides for packages that need C deps
    };
    python-set = (pkgs.callPackage pyproject-nix.build.packages {
      inherit python;
    }).overrideScope (pkgs.lib.composeManyExtensions [
      overlay
      pyprojectOverrides
    ]);

    # Build the virtual environment
    virtualenv = python-set.mkVirtualEnv "my-app-env" workspace.deps.default;

  in {
    packages.x86_64-linux.default = virtualenv;

    devShells.x86_64-linux.default = pkgs.mkShell {
      packages = [ virtualenv pkgs.uv ];
    };
  };
}
```

**Key concepts:**

- **`workspace.loadWorkspace`**: Reads `pyproject.toml` and `uv.lock` from `workspaceRoot`. Returns a workspace object.
- **`workspace.mkPyprojectOverlay`**: Creates a nixpkgs overlay that populates `python-set` with all packages from `uv.lock` as Nix derivations.
- **`sourcePreference`**: `"wheel"` prefers pre-built wheels (faster, fewer C dep issues); `"sdist"` builds from source (more reproducible, may require C build tools).
- **`python-set.mkVirtualEnv`**: Creates a virtual environment derivation containing all specified dependency groups.
- **`workspace.deps.default`**: The default dependency group (production deps, equivalent to `[project.dependencies]` in pyproject.toml).

**uv.lock is the single source of truth.** Unlike `poetry2nix` which parses `poetry.lock`, `uv2nix` reads `uv.lock` directly. The lockfile is generated by `uv lock` and committed to the repository.

**Updating dependencies:**

```bash
uv add requests       # adds to pyproject.toml and updates uv.lock
uv lock --upgrade     # upgrade all deps within constraints
# Then re-enter nix develop — the overlay re-reads uv.lock
```

**Limitation:** Packages with C extensions that aren't available as wheels and have incorrect `pyproject.toml` metadata need manual overrides in `pyprojectOverrides`. This is the same problem poetry2nix solved with `defaultPoetryOverrides`, but uv2nix does not yet ship a comparable default override set.

---

## Part 4: Nix Code Formatter Ecosystem

### 4.1 Overview: Three Formatters Competing

As of 2025-2026, three Nix formatters exist with overlapping purposes but different design philosophies:

| Formatter | Language | Status | Style |
|-----------|----------|--------|-------|
| `nixpkgs-fmt` | Rust | Deprecated/archived | Permissive |
| `alejandra` | Rust | Active, widely used | Opinionated |
| `nixfmt` (`nixfmt-rfc-style`) | Haskell | Active, official, being adopted by nixpkgs | Standardized via RFC |

### 4.2 `nixpkgs-fmt` (Deprecated)

`nixpkgs-fmt` was the first automated Nix formatter. It was used by nixpkgs CI for a period. As of 2024-2025 it is **deprecated** — the repository is archived. New projects should not use it.

Available in nixpkgs as `pkgs.nixpkgs-fmt` but being phased out.

### 4.3 `alejandra`: Opinionated Rust Formatter

**`alejandra`** (https://github.com/kamadorueda/alejandra) is the most widely used Nix formatter in community projects. Version 4.0.0 was released April 2025.

**Characteristics:**
- Rust-based; formats the entire nixpkgs in seconds
- Completely deterministic: same input → same output, always
- Opinionated: imposes its style without configuration knobs
- Semantic equivalence: formatted code is always semantically identical to input (no AST-level changes)
- Unlicense (public domain)

**Flake integration (two patterns):**

```nix
# Pattern 1: Use alejandra from nixpkgs (simplest)
{
  outputs = { nixpkgs, ... }:
  let pkgs = import nixpkgs { system = "x86_64-linux"; }; in {
    formatter.x86_64-linux = pkgs.alejandra;
  };
}
# nix fmt → runs alejandra over all .nix files in the flake directory

# Pattern 2: Pin a specific alejandra version
{
  inputs.alejandra = {
    url = "github:kamadorueda/alejandra/4.0.0";
    inputs.nixpkgs.follows = "nixpkgs";
  };
  outputs = { alejandra, ... }: {
    formatter.x86_64-linux = alejandra.defaultPackage.x86_64-linux;
  };
}
```

**`nix fmt` scope:** `nix fmt` invokes `formatter.<system>` on the flake's directory. It only passes `.nix` files in the flake root by default. For multi-language formatting, wrap with `treefmt` (see `docker-vms-scripts-fmt-security.md`).

**Editor integration:** LSP via `nil` or `nixd` calls alejandra for on-save formatting. Configured via `editor.formatOnSave = true` and the relevant formatter setting.

### 4.4 `nixfmt` / `nixfmt-rfc-style`: The Standardization Path

**`nixfmt`** (https://github.com/NixOS/nixfmt) is now an official Nix project, originally developed by Serokell. The `nixfmt-rfc-style` package in nixpkgs is the RFC-compliant variant of nixfmt implementing the formatting standard defined in `standard.md`.

**RFC 166:** Accepted. Establishes nixfmt as the official formatting standard for Nix code. The "rfc-style" variant enforces the RFC's rules.

**nixpkgs adoption status (2025-2026):** nixpkgs is in the process of adopting `nixfmt-rfc-style` as the standard formatter for the repository itself. This is a multi-step process: formatcheck CI, then a mass-reformat PR, then enforcement. As of early 2026 the migration is underway but not yet complete across the entire nixpkgs tree.

**Using nixfmt-rfc-style:**

```nix
# As flake formatter
{
  outputs = { nixpkgs, ... }:
  let pkgs = import nixpkgs { system = "x86_64-linux"; }; in {
    formatter.x86_64-linux = pkgs.nixfmt-rfc-style;
  };
}
```

```bash
# CLI usage
nixfmt file.nix              # format in-place
nixfmt --check file.nix      # exit 1 if not formatted
nixfmt --verify file.nix     # format and verify semantic equivalence
```

**Differences from alejandra:**

| Dimension | alejandra | nixfmt-rfc-style |
|-----------|-----------|-----------------|
| Implementation | Rust | Haskell |
| Configurability | None | Minimal (width) |
| Line width | Fixed narrow | Configurable (default 100) |
| Attribute set layout | Compact | More vertical |
| Official status | Community | RFC-standardized |
| nixpkgs adoption | Previously common | Being adopted now |
| `nixfmt-tree` support | No | Yes (format entire directory tree) |

**`nixfmt-tree`:** A companion tool that runs nixfmt recursively over a directory, respecting `.git` and `.gitignore`. Useful for CI:

```bash
nixfmt-tree --check .   # verify all .nix files are formatted
```

**treefmt integration:**

```nix
# treefmt-nix module
treefmt.programs.nixfmt-rfc-style.enable = true;
# or
treefmt.programs.alejandra.enable = true;  # pick one
```

### 4.5 `nix fmt` Hook Semantics

`formatter.<system> = pkgs.alejandra` makes `nix fmt` invoke `alejandra .` where `.` is the flake root directory. The formatter binary receives the directories/files to format as positional arguments. Alejandra and nixfmt both accept directories and recurse into them.

**Only `.nix` files are formatted** by default (each formatter determines which files it processes based on extension).

**`nix fmt` does not pass `--check`** — it always reformats in-place. For CI enforcement use `treefmt` with `--fail-on-change` or invoke the formatter with its own `--check` flag directly.

**Pre-commit hook pattern:**

```yaml
# .pre-commit-config.yaml
repos:
  - repo: local
    hooks:
      - id: nixfmt
        name: nixfmt
        language: system
        entry: nixfmt --check
        files: \.nix$
```

Or via `git-hooks.nix` (formerly `pre-commit-hooks.nix`) as a flake check:

```nix
checks.x86_64-linux.pre-commit = git-hooks.lib.x86_64-linux.run {
  src = ./.;
  hooks.alejandra.enable = true;
  # or
  hooks.nixfmt-rfc-style.enable = true;
};
```

### 4.6 Choosing a Formatter

**For new projects (2026 recommendation):**
- Use `nixfmt-rfc-style` if you want to align with the direction nixpkgs is heading and RFC standardization matters to you.
- Use `alejandra` if you want the most widely deployed formatter with the largest community adoption outside nixpkgs itself.
- Do not use `nixpkgs-fmt` (archived).

**For nixpkgs contributions:** Watch the nixpkgs formatting CI; as the migration completes, nixfmt-rfc-style will be required.

**In `flake.nix` for forge-metal:** The current repo uses alejandra (as part of treefmt-nix). If adopting nixfmt-rfc-style later, `nix fmt` on the entire tree and a single commit are sufficient since both formatters are idempotent.

---

## Summary Table

| Topic | Key Takeaway |
|-------|-------------|
| `nix search` | Evaluates `legacyPackages` recursively; eval-cache-v6 makes repeat searches fast; cold cache takes 30–90s |
| `nix search --json` | Output: `{ "<attrPath>": { pname, version, description } }` |
| `nix-index` | Indexes binary cache NAR file lists; `nix-locate bin/ffmpeg` to find provider |
| `nix-index-database` | Weekly pre-built database as flake; `programs.nix-index-database.comma.enable = true` |
| comma (`,`) | Uses nix-index to find package then runs via `nix shell`; caches aggressively |
| `nix-search-cli` | Queries search.nixos.org API directly; no local eval needed |
| `fetchClosure` pattern 1 (CA) | No trust config needed; works in pure mode; preferred |
| `fetchClosure` pattern 2 (IA→CA) | Rewrite to CA via `toPath`; find toPath with `nix store make-content-addressed` |
| `fetchClosure` pattern 3 (IA) | Requires `trusted-public-keys` or `require-sigs = false`; avoid |
| `withPackages` | Wraps `python.buildEnv`; creates interpreter wrapper with `PYTHONPATH` set |
| `python3.pkgs` | Same as `python3Packages`; scoped to that interpreter version |
| poetry2nix | Unmaintained; do not use for new projects |
| uv2nix | Current recommended approach; reads `uv.lock`; pure Nix derivations |
| nixpkgs-fmt | Archived; deprecated |
| alejandra | Most widely used community formatter; opinionated; v4.0.0 April 2025 |
| nixfmt-rfc-style | Official RFC-standardized formatter; nixpkgs migrating to it in 2025-2026 |
