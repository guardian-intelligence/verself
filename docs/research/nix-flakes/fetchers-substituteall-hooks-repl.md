# Nix Flakes: Source Fetchers, substituteAll/replaceVars, Setup Hooks, nix repl Advanced

Covers: `pkgs.fetchFromGitHub` and the full fetcher family, `pkgs.substituteAll` vs `pkgs.replaceVars` template substitution, the `pkgs.makeSetupHook` system and how setup hooks propagate through the build environment, and advanced `nix repl` usage patterns.

---

## Topic 1: nixpkgs Source Fetchers — `fetchFromGitHub` and Family

Fetchers are fixed-output derivations (FODs) that download source code from the network. Every fetcher requires declaring the hash of its output in advance; the Nix build sandbox verifies the download matches the hash.

### `pkgs.fetchurl` — Single-File FOD

The foundation of the fetcher stack. All other fetchers eventually delegate to it.

```nix
fetchurl {
  url = "https://example.org/hello-2.12.tar.gz";
  hash = "sha256-1aAjkHgBD6GaM/rYPFMNHoUHMxFfSQZ0Yh5dHAwR34=";
}
```

**All parameters:**

| Parameter | Type | Default | Purpose |
|-----------|------|---------|---------|
| `url` | string | `""` | Single download URL |
| `urls` | list | `[]` | Multiple mirror URLs tried in order |
| `hash` | SRI string | — | Preferred modern hash format |
| `outputHash` + `outputHashAlgo` | string pair | — | Explicit algorithm/value alternative |
| `sha1`/`sha256`/`sha512` | string | — | Legacy individual hash attrs |
| `curlOpts` | string | `""` | Space-separated curl flags (deprecated) |
| `curlOptsList` | list | `[]` | Individual curl args (preferred; handles spaces) |
| `netrcPhase` | shell | `null` | Script that generates `.netrc` for basic auth |
| `netrcImpureEnvVars` | list | `[]` | Env vars passed to `netrcPhase` |
| `postFetch` | shell | `""` | Commands run after successful download |
| `downloadToTemp` | bool | `false` | Download to `$downloadedFile` temp path instead of `$out` |
| `executable` | bool | `false` | Set executable bit on output (forces `recursive` hash mode) |
| `recursiveHash` | bool | `false` | Force NAR hash mode |
| `name` | string | URL basename | Derivation name |
| `pname` + `version` | strings | — | Combined as `${pname}-${version}` for `name` |
| `passthru` | attrset | `{}` | Attributes passed through to result |
| `meta` | attrset | `{}` | Package metadata |

**`outputHashMode` selection:** `"recursive"` (NAR hash) when `executable = true` or `recursiveHash = true`; otherwise `"flat"` (raw file hash).

**Authenticated downloads:**

```nix
# Using curlOptsList (preserves spaces correctly)
fetchurl {
  url = "https://example.com/private.tar.gz";
  hash = "sha256-...";
  curlOptsList = [ "--user" "myuser:mypassword" ];
}

# Using netrcPhase (credentials from environment at build time)
fetchurl {
  url = "https://example.com/artifact.tar.gz";
  hash = "sha256-...";
  netrcPhase = ''
    echo "machine example.com login $MY_USER password $MY_PASS" > .netrc
  '';
  netrcImpureEnvVars = [ "MY_USER" "MY_PASS" ];
}
```

**`impureEnvVars`** automatically propagated by `fetchurl`:
- `lib.fetchers.proxyImpureEnvVars` — `http_proxy`, `https_proxy`, `ftp_proxy`, `no_proxy`, etc.
- `NIX_CURL_FLAGS` — extra curl flags from the host environment
- `NIX_HASHED_MIRRORS` — hash-based mirror override
- `NIX_CONNECT_TIMEOUT` — timeout for hashed mirror connections
- `NIX_MIRRORS_${site}` — per-mirror URL overrides

**Mirror URLs:** `fetchurl` expands `mirror://` prefixes using the mirror registry at `pkgs/build-support/fetchurl/mirrors.nix`. Example: `mirror://gnu/hello/hello-2.12.tar.gz` expands to multiple GNU mirrors.

**SSL:** TLS certificate verification is disabled only when `hash` is empty, fake (`lib.fakeHash`), or when credentials are required — otherwise the system CA bundle is used.

**FOD mechanics:** The output path is computed from the declared `outputHash` (not the inputs), so `fetchurl` always produces the same store path for the same hash regardless of the URL. This is why hash stability matters more than URL stability.

---

### `pkgs.fetchzip` — Archive-Unpacking FOD

Wraps `fetchurl` to download and unpack archives (tarballs, zip files, `.rar`, etc.). The `hash` covers the NAR of the **unpacked directory**, not the tarball bytes.

```nix
fetchzip {
  url = "https://example.org/source-1.0.tar.gz";
  hash = "sha256-AAAA...";   # NAR hash of unpacked contents
}
```

**Parameters beyond `fetchurl`:**

| Parameter | Default | Behavior |
|-----------|---------|----------|
| `stripRoot` | `true` | Strip the single top-level directory (most archives have one); error if archive has multiple top-level items |
| `stripRoot = false` | — | Preserve archive structure as-is |
| `extension` | auto-detected | Hint for unpacking tool selection when URL doesn't reveal type |
| `postFetch` | `""` | Shell commands after unpacking; `$out` contains unpacked dir |
| `extraPostFetch` | deprecated | Renamed to `postFetch` (warning emitted) |
| `withUnzip` | `true` | Include unzip binary (disable to reduce closure) |

**When to use `stripRoot = false`:**
```nix
# When archive contains multiple top-level items
fetchzip {
  url = "https://example.org/flat-release.zip";
  hash = "sha256-...";
  stripRoot = false;
}
```

**Hash stability note:** The hash covers the NAR of the unpacked tree, not the compressed archive. Two tarballs with identical contents but different compression produce the same hash — which is why `fetchFromGitHub` (using `fetchzip`) has more stable hashes than `fetchFromGitHub` with `fetchSubmodules = true` (using `fetchgit`). The nixpkgs docs explicitly state this: "We prefer fetchzip in cases we don't need submodules as the hash is more stable in that case."

---

### `pkgs.fetchFromGitHub` — The Canonical Source Fetcher

Used in the vast majority of nixpkgs packages. Downloads `github.com/<owner>/<repo>/archive/<rev>.tar.gz` via `fetchzip` by default — **not** a git clone.

```nix
fetchFromGitHub {
  owner = "BurntSushi";
  repo  = "ripgrep";
  rev   = "14.1.0";
  hash  = "sha256-...";   # NAR hash of unpacked tarball
}
```

**All parameters:**

| Parameter | Type | Notes |
|-----------|------|-------|
| `owner` | string | Required. GitHub username or org. |
| `repo` | string | Required. Repository name. |
| `rev` | string | Required (exclusive with `tag`). Commit SHA or tag name. |
| `tag` | string | Required (exclusive with `rev`). Git tag. Equivalent to `rev` but explicit. |
| `hash` | SRI string | Required. NAR hash of unpacked tarball. |
| `githubBase` | string | Default `"github.com"`. For GitHub Enterprise: `"git.corp.example.com"`. |
| `private` | bool | Default `false`. Switches to API endpoint for private repo access. |
| `varPrefix` | string | Env var prefix for multi-repo expressions. |
| `fetchSubmodules` | bool | Default `false`. Switches to `fetchgit` (slower). |
| `deepClone` | bool | Default `false`. Full clone (implies `leaveDotGit`). Switches to `fetchgit`. |
| `fetchLFS` | bool | Default `false`. Enable Git LFS. Switches to `fetchgit`. |
| `leaveDotGit` | bool | Default `false`. Keep `.git` dir. Switches to `fetchgit`. |
| `sparseCheckout` | list | Directories to include. Switches to `fetchgit` with `--filter=blob:none`. |
| `forceFetchGit` | bool | Default `false`. Force `fetchgit` even without submodules. |
| `passthru` | attrset | Passed through to result. |
| `meta` | attrset | Package metadata. |

**`fetchzip` vs `fetchgit` delegation:**

The `useFetchGit` flag is `true` if any of: `fetchSubmodules`, `deepClone`, `fetchLFS`, `leaveDotGit`, `sparseCheckout`, or `forceFetchGit` are set. Otherwise `fetchzip` is used.

```nix
# Default: fetchzip (fast, stable hash, no git history)
fetchFromGitHub { owner = "foo"; repo = "bar"; rev = "v1.0"; hash = "..."; }

# With submodules: fetchgit (slow full clone; only when truly needed)
fetchFromGitHub {
  owner = "foo"; repo = "bar"; rev = "v1.0"; hash = "...";
  fetchSubmodules = true;
  # hash now covers NAR of entire cloned tree including submodules
}

# Sparse checkout: large monorepo, only fetch specific directories
fetchFromGitHub {
  owner = "microsoft"; repo = "fluentui"; rev = "v9.0.0"; hash = "...";
  sparseCheckout = [ "packages/react-components" "packages/react-icons" ];
}
```

**Private repos (GitHub Enterprise or private.com):**

```nix
fetchFromGitHub {
  githubBase = "github.mycompany.com";
  owner = "myorg"; repo = "private-lib"; rev = "abc123"; hash = "...";
  private = true;
  # Uses https://github.mycompany.com/api/v3/repos/myorg/private-lib/tarball/abc123
}
```

---

### `pkgs.fetchFromGitLab`, `fetchFromSourcehut`, `fetchFromBitbucket`

All follow the same pattern as `fetchFromGitHub`.

**`pkgs.fetchFromGitLab`:**
```nix
fetchFromGitLab {
  owner = "inkscape";
  repo  = "inkscape";
  rev   = "INKSCAPE_1_3";
  hash  = "sha256-...";
  # domain = "gitlab.gnome.org";   # for self-hosted GitLab
}
```
- `domain` parameter (default `"gitlab.com"`) enables self-hosted instances.
- `group` parameter for subgroup paths (`group/subgroup/repo`).
- `fetchSubmodules` supported (switches to `fetchgit`).

**`pkgs.fetchFromSourcehut`:**
```nix
fetchFromSourcehut {
  owner = "~sircmpwn";   # tilde prefix required
  repo  = "harelang";
  rev   = "0.24.0";
  hash  = "sha256-...";
  # vc = "hg";  # default is "git"; set to "hg" for Mercurial repos
}
```

**`pkgs.fetchFromBitbucket`:**
```nix
fetchFromBitbucket {
  owner = "pypy";
  repo  = "pypy";
  rev   = "v7.3.13";
  hash  = "sha256-...";
}
```

---

### `pkgs.fetchgit` — Full Git Clone FOD

Used when you need git history, tags, submodules, or LFS. Significantly slower than `fetchzip`-based fetchers because it performs a full or partial git clone.

```nix
fetchgit {
  url = "https://github.com/nixos/nixpkgs.git";
  rev = "abc123def456";
  hash = "sha256-...";  # NAR hash; outputHashMode = "recursive"
}
```

Key parameters not in `fetchzip`:
- `fetchSubmodules` — default `true` in `fetchgit` (note: default `false` in `fetchFromGitHub`'s delegating call)
- `leaveDotGit` — keep `.git` directory; required for `fetchTags`; conflicts with `rootDir`
- `deepClone` — full `git clone` (implies `leaveDotGit = true`)
- `fetchTags` — fetch all tags (requires `leaveDotGit = true`)
- `sparseCheckout` — list of paths; uses `--filter=blob:none --sparse`
- `rootDir` — extract only a subdirectory (requires `leaveDotGit = false`)
- `branchName` — hint for remote tracking; mostly for disambiguation
- `fetchLFS` — fetch Git LFS objects (slower)
- `postCheckout` — shell code run after checkout, before hashing
- `gitConfigFile` — path to a git config file injected during clone

---

### `pkgs.fetchsvn`, `pkgs.fetchhg` — Legacy VCS Fetchers

```nix
# Subversion
fetchsvn {
  url = "https://svn.example.org/repos/project";
  rev = "12345";
  hash = "sha256-...";
}

# Mercurial
fetchhg {
  url = "https://hg.example.org/repo";
  rev = "abc123";
  hash = "sha256-...";
}
```

Rare but still present in nixpkgs for packages that predate git adoption or are maintained in SVN/Hg. Both use `outputHashMode = "recursive"`.

---

### `pkgs.fetchPypi` — PyPI Source Distribution Fetcher

```nix
fetchPypi {
  pname   = "requests";
  version = "2.31.0";
  hash    = "sha256-...";
}
# Downloads from:
# mirror://pypi/r/requests/requests-2.31.0.tar.gz
# → expanded to https://files.pythonhosted.org/packages/...
```

**Parameters:**
- `pname`, `version` — required; used to construct URL
- `extension` — default `"tar.gz"`; use `"zip"` for some packages
- `format` — `"setuptools"` (default) or `"wheel"`
- For wheel format: `dist`, `python`, `abi`, `platform` control the wheel filename
- `hash` — SRI hash of the sdist/wheel

The URL construction uses PyPI's `mirror://pypi` scheme which expands to `files.pythonhosted.org`. The function uses `lib.makeOverridable` to allow downstream callers to override any attribute.

---

### `pkgs.fetchCrate` — crates.io Fetcher

```nix
fetchCrate {
  pname   = "serde";
  version = "1.0.193";
  hash    = "sha256-...";
}
# Downloads from:
# https://crates.io/api/v1/crates/serde/1.0.193/download
```

**Parameters:**
- `pname` — required; becomes `crateName` if not overridden
- `version` — required
- `crateName` — defaults to `pname`; override for crates with different Cargo name
- `registryDl` — default `"https://crates.io/api/v1/crates"`; override for private registries
- `unpack` — default `true`; delegates to `fetchzip`; `false` uses `fetchurl`
- `hash` — SRI hash of the unpacked crate directory (when `unpack = true`)

The crate archive format is `.tar.gz` containing a single `{pname}-{version}/` directory (which `stripRoot = true` strips automatically).

---

### The `hash` vs `sha256` Attribute Migration

nixpkgs migrated from algorithm-specific hash attributes to the SRI (Subresource Integrity) format starting around nixpkgs 22.05 and completed progressively through 2023.

```nix
# Old style (still works but deprecated)
fetchFromGitHub {
  owner = "foo"; repo = "bar"; rev = "v1.0";
  sha256 = "0xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx";
  # or: sha256 = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";  # base64
}

# New style (preferred)
fetchFromGitHub {
  owner = "foo"; repo = "bar"; rev = "v1.0";
  hash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
}
```

The SRI format is `algorithm-base64encodedHash=`. Both `sha256` (old) and `hash` (new) are currently accepted; new packages should use `hash`.

**Finding the hash for a new package:**

```bash
# Method 1: Use lib.fakeHash — build fails with the correct hash
nix-prefetch-url --unpack https://github.com/foo/bar/archive/v1.0.tar.gz

# Method 2: Run with wrong hash, get correct hash from error
nix build --impure --expr 'with import <nixpkgs> {}; fetchFromGitHub { owner="foo"; repo="bar"; rev="v1.0"; hash=lib.fakeHash; }'
# error: hash mismatch in fixed-output derivation:
#   specified: sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
#   got:       sha256-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=

# Method 3: nix-prefetch-github (external tool)
nix run nixpkgs#nix-prefetch-github -- foo bar --rev v1.0
```

`lib.fakeHash` is the SRI zero value: `sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=`. It's distinct from `lib.fakeSha256` (which is the hex zero value for the legacy `sha256` attribute).

---

## Topic 2: `pkgs.substituteAll` and `pkgs.replaceVars` — Template Substitution

These functions generate files (typically scripts or config files) with embedded Nix store paths, replacing `@placeholder@` markers with actual values.

### The Substitution Primitive: `pkgs.substitute`

The lowest-level Nix wrapper. It calls the `substitute` bash built-in (part of stdenv) with an explicit list of `--replace-fail`/`--subst-var-by` flags.

```nix
# pkgs/build-support/substitute/substitute.nix
substitute {
  src           = ./greeting.sh;
  name          = "greeting-configured.sh";   # optional, defaults to src filename
  substitutions = [
    "--replace-fail" "@greeting@" "Hello, World"
    "--subst-var-by" "shell" "${bash}/bin/bash"
  ];
}
```

**Parameters:**
- `src` — source file path
- `substitutions` — list of arguments passed to the `substitute` binary; must be a list (not a string) to handle spaces correctly
- `replacements` — deprecated alias; space-separated string that was the old interface; emits a warning on nixpkgs ≥ 24.05
- `name` — output derivation name; defaults to the filename part of `src`

**Implementation:** `stdenvNoCC.mkDerivation` with `preferLocalBuild = true`, `allowSubstitutes = false`. The builder calls `substitute $src $target ${substitutions}`.

The `substitute` binary (a compiled C tool in stdenv) supports these flags:
- `--replace-fail OLD NEW` — replace all occurrences; **fail** if OLD not found
- `--replace OLD NEW` — replace all occurrences; **no failure** if OLD not found
- `--subst-var VAR` — replace `@VAR@` with the shell variable `$VAR`
- `--subst-var-by VAR VAL` — replace `@VAR@` with literal VAL

---

### `pkgs.substituteAll` — Environment-Variable-Based Replacement

Automatically replaces **all** `@varName@` markers using the **current environment variables** plus derivation attributes. The source file acts as a template; any lowercase variable in the environment or derivation attrs with a matching `@name@` placeholder gets substituted.

```nix
# In a package's default.nix:
substituteAll {
  src    = ./wrapper.sh;          # contains @ffmpeg@ and @python3@
  ffmpeg = pkgs.ffmpeg;           # @ffmpeg@ → /nix/store/...-ffmpeg-6.0/
  python3 = pkgs.python3;         # @python3@ → /nix/store/...-python3-3.11.6/
}
```

The resulting derivation is a file with `/nix/store/` paths baked in, not environment variables.

**How it works:** The `substitute-all.sh` builder passes the entire set of environment variables (set from the derivation's attrs) to the `substitute` binary via `--subst-var` for each variable. Any `@name@` in the source file that matches a variable name is replaced.

**Silent-skip semantics:** `substituteAll` **does not error** on `@placeholder@` markers that have no matching variable. They remain in the output unchanged. This is the key difference from `replaceVars`.

**Common pattern — wrapper scripts:**

```bash
# wrapper.sh (template)
#!@bash@/bin/bash
exec @ffmpeg@/bin/ffmpeg "$@"
```

```nix
# package/default.nix
{ stdenv, bash, ffmpeg, substituteAll }:
stdenv.mkDerivation {
  name = "my-tool";
  src  = ./wrapper.sh;
  nativeBuildInputs = [ substituteAll ];
  # Actually, it's used like this in practice:
  installPhase = ''
    install -m755 ${substituteAll {
      src  = ./wrapper.sh;
      bash = bash;
      ffmpeg = ffmpeg;
    }} $out/bin/my-tool
  '';
}
```

**`pkgs.substituteAllFiles`:** Like `substituteAll` but processes a **directory** recursively. All files in the directory have `@var@` substitutions applied.

```nix
substituteAllFiles {
  src  = ./config-template-dir;
  files = [ "config.conf" "wrapper.sh" ];   # optional filter; all files if omitted
  # additional attrs for substitution:
  dataDir = "/var/lib/myapp";
}
```

---

### `pkgs.replaceVars` — Strict Template Substitution (Preferred)

Added in nixpkgs ~23.11 as the **successor to `substituteAll`**. Uses the same `@varName@` syntax but with stricter validation: any `@placeholder@` remaining in the output after substitution (matching `@[a-zA-Z_][0-9A-Za-z_'-]*@`) causes a **build failure**.

```nix
# New code should use replaceVars instead of substituteAll
replaceVars ./wrapper.sh {
  bash   = pkgs.bash;
  ffmpeg = pkgs.ffmpeg;
}
# Build FAILS if wrapper.sh contains @someVar@ not in the attrset
```

**Full signature:**

```nix
replaceVars src replacements
# where replacements is an attrset: { varName = value; ... }

# or the lower-level replaceVarsWith:
replaceVarsWith {
  src          = ./file.sh;
  replacements = { ffmpeg = pkgs.ffmpeg; };
  dir          = "bin";          # optional: output goes to $out/bin/
  isExecutable = true;           # optional: chmod +x
}
```

**Null values — opt out of validation:**

```nix
replaceVars ./config.conf {
  apiEndpoint = "https://api.example.com";
  # Skip the check for @version@ — left in file intentionally
  version = null;
}
```

Setting a value to `null` excludes it from the post-substitution check, allowing selective `@placeholder@` preservation.

**Implementation details (`replaceVarsWith`):**
- Uses `--replace-fail` for each substitution (fails if the target text is not found — catching typos in variable names)
- After substitution runs a check phase scanning for remaining `@[a-zA-Z_][0-9A-Za-z_'-]*@` patterns
- `doCheck = true`, `dontUnpack = true`, `preferLocalBuild = true`, `allowSubstitutes = false`
- `stdenvNoCC.mkDerivation` base

**`substituteAll` vs `replaceVars` comparison:**

| Aspect | `substituteAll` | `replaceVars` |
|--------|-----------------|---------------|
| Syntax | `substituteAll src { ... }` | `replaceVars src { ... }` |
| Unknown `@var@` in source | Silently left in output | Build failure |
| Unused replacement in attrset | Silently ignored | Build failure (`--replace-fail`) |
| Null values | N/A | Skips validation for that placeholder |
| When introduced | Nixpkgs original | ~nixpkgs 23.11 |
| Recommended for new code | No | Yes |

---

### `makeWrapper` vs `replaceVars` — Choosing the Right Tool

Both inject store paths into executable wrappers, but via different mechanisms:

**`wrapProgram` / `makeWrapper`** (runtime PATH injection):
```nix
# In fixupPhase or postInstall:
wrapProgram $out/bin/myapp \
  --prefix PATH : ${lib.makeBinPath [ ffmpeg python3 ]} \
  --set MYAPP_DATA $out/share/myapp \
  --run "export TMPDIR=/tmp"
```
- Creates a shell wrapper that sets `PATH`, env vars, and then `exec`s the original binary
- Original binary is moved to `.myapp-wrapped`; wrapper is placed at original path
- Does **not** require modifying the source file
- Adds a shell interpreter invocation overhead per execution (~1ms)
- `wrapProgram` is built on `makeWrapper`; `wrapProgram` accepts all flags except `--inherit-argv0` and `--argv0`

**`replaceVars`** (build-time path embedding):
```bash
#!/@bash@/bin/bash
exec @ffmpeg@/bin/ffmpeg "$@"
```
```nix
replaceVars ./wrapper.sh { bash = pkgs.bash; ffmpeg = pkgs.ffmpeg; }
```
- Embeds store paths literally into the file at build time
- No runtime overhead — the script already contains absolute paths
- More hermetic: the script cannot be broken by PATH changes
- Requires the source file to use `@placeholder@` markers
- The script itself must be written to reference explicit paths

**Rule of thumb:**
- Use `wrapProgram` when you don't control the source (wrapping a compiled binary from an upstream project)
- Use `replaceVars` when you're writing the wrapper script yourself (your own shell scripts that need Nix-managed dependencies)

---

## Topic 3: `pkgs.makeSetupHook` and the Setup Hook System

### How Setup Hooks Work

A **setup hook** is a bash script in `$pkg/nix-support/setup-hook`. When a package is listed in `nativeBuildInputs` or `buildInputs` of another derivation, stdenv's `setup.sh` automatically **sources** that hook into the build environment.

This is the mechanism by which `cmake`, `pkg-config`, `meson`, Python, etc. configure themselves without explicit per-package glue. Adding `cmake` to `nativeBuildInputs` makes `cmake`-based builds work because `cmake`'s setup hook adds the necessary `CMAKE_PREFIX_PATH` entries.

**The `activatePackage` function in `setup.sh`:**
```bash
# From pkgs/stdenv/generic/setup.sh
if [[ -f "$pkg/nix-support/setup-hook" ]]; then
    nixTalkativeLog "sourcing setup hook '$pkg/nix-support/setup-hook'"
    source "$pkg/nix-support/setup-hook"
fi
```

This runs during the `_activatePkgs` phase (before any build phases), after all inputs have been registered via `findInputs`.

---

### Hook Execution Order

Dependencies are activated in this order (from `setup.sh`):

1. `depsBuildBuild` + `depsBuildBuildPropagated`
2. `nativeBuildInputs` + `propagatedNativeBuildInputs`
3. `depsBuildTarget` + `depsBuildTargetPropagated`
4. `depsHostHost` + `depsHostHostPropagated`
5. `buildInputs` + `propagatedBuildInputs`
6. `depsTargetTarget` + `depsTargetTargetPropagated`

Within each category, hooks from dependencies run **before** the package's own `preBuild`, `buildPhase`, etc. hooks. This means a `nativeBuildInput`'s setup hook has already run by the time any custom `buildPhase` executes.

**Cross-compilation note:** In cross builds (`stdenv.hostPlatform != stdenv.buildPlatform`), `buildInputs` setup hooks are **not** run unconditionally — they only run if `strictDeps` is unset. `nativeBuildInputs` hooks always run regardless.

---

### Propagated Dependencies — Transitive Hook Sourcing

The `nix-support/propagated-build-inputs` and `nix-support/propagated-native-build-inputs` files contain space-separated lists of store paths. The `findInputs` function reads these files recursively to discover transitive dependencies whose setup hooks must also be sourced.

```
# /nix/store/...-openssl-3.1.4/nix-support/propagated-build-inputs
/nix/store/...-zlib-1.3
```

When package A depends on openssl (which propagates zlib), and package B depends on A, then building B automatically sources:
- A's setup hook
- openssl's setup hook
- zlib's setup hook (via propagation)

**`propagatedBuildInputs` vs `propagatedNativeBuildInputs`:** The former propagates libraries needed at runtime; the latter propagates build tools. For a Python package, `propagatedBuildInputs` ensures dependent packages also get the Python library in their `PYTHONPATH`.

The `recordPropagatedDependencies` function (called in every `stdenv.mkDerivation` build) writes the accumulated propagated dependency lists to `$out/nix-support/propagated-*-inputs` for downstream packages to discover.

---

### `pkgs.makeSetupHook` — Creating Custom Setup Hooks

```nix
pkgs.makeSetupHook {
  name                       = "my-framework-setup-hook";
  propagatedBuildInputs      = [ pkgs.someLib ];
  propagatedNativeBuildInputs = [ pkgs.someTool ];
  depsTargetTargetPropagated = [];        # for cross-compilation target deps
  substitutions              = {          # substituteAll-style replacements
    myFrameworkLib = "${myFramework}/lib";
  };
  meta    = { description = "Setup hook for my-framework"; };
  passthru = {};
}
./my-framework-setup-hook.sh     # the script to use as the hook
```

**Parameters:**

| Parameter | Default | Purpose |
|-----------|---------|---------|
| `name` | deprecated warn | Derivation name; required for new code |
| `propagatedBuildInputs` | `[]` | Build inputs transitive to dependents |
| `propagatedNativeBuildInputs` | `[]` | Native build tools transitive to dependents |
| `depsTargetTargetPropagated` | `[]` | Cross-compilation target deps |
| `substitutions` | `{}` | `substituteAll`-style replacements in the hook script |
| `meta` | `{}` | Package metadata |
| `passthru` | `{}` | Passthrough attributes |

**The `script` argument:** The function is curried — `makeSetupHook { ... } ./script.sh`. The script is the last positional argument.

**Implementation:** Creates a small derivation via `runCommand` that:
1. Creates `$out/nix-support/`
2. Copies the script to `$out/nix-support/setup-hook` (or runs `substituteAll` on it if `substitutions != {}`)
3. Calls `recordPropagatedDependencies` to write propagated deps files

The hook script itself is plain bash. It can define functions, export variables, and call `addToSearchPath` or `addEnvHooks`:

```bash
# my-framework-setup-hook.sh
myFrameworkSetupHook() {
  addToSearchPath CMAKE_PREFIX_PATH "$1/cmake"
  addToSearchPath PKG_CONFIG_PATH "$1/lib/pkgconfig"
}

addEnvHooks "$targetOffset" myFrameworkSetupHook
```

The `addEnvHooks` call registers `myFrameworkSetupHook` to be called with each package path when dependencies are activated. The `$targetOffset` variable (0 for host, -1 for build, 1 for target) ensures correct cross-compilation behavior.

---

### Real Examples from nixpkgs

**`pkg-config` setup hook** (`pkgs/development/tools/pkg-config/setup-hook.sh`):
```bash
pkgConfigSetupHook() {
  addToSearchPath PKG_CONFIG_PATH "$1/lib/pkgconfig"
  addToSearchPath PKG_CONFIG_PATH "$1/share/pkgconfig"
}
addEnvHooks "$hostOffset" pkgConfigSetupHook
```
Every package in `buildInputs` that has a `.pc` file gets its path added to `PKG_CONFIG_PATH`. This is why `pkg-config` works without any per-package configuration.

**`cmake` setup hook** adds `CMAKE_PREFIX_PATH` entries pointing to installed packages.

**Python setup hook** adds `PYTHONPATH` entries from `propagatedBuildInputs`. This is how `buildPythonPackage`'s `propagatedBuildInputs` works transitively — each Python package's setup hook extends `PYTHONPATH` for its dependents.

**`installShellFiles` setup hook** (from `pkgs/build-support/install-shell-files/`) provides the `installShellCompletion` bash function as a setup hook.

---

### `passthru.setupHook` Convention

When a package wants to expose a reusable setup hook as part of its public API:

```nix
myLibrary = stdenv.mkDerivation {
  # ... main package definition ...
  passthru.setupHook = pkgs.makeSetupHook {
    name = "my-library-setup-hook";
    propagatedBuildInputs = [ myLibrary ];
  } ./setup-hook.sh;
};
```

Consumers can then access `pkgs.myLibrary.setupHook` and add it to their `nativeBuildInputs`.

---

## Topic 4: `nix repl` Advanced Usage

`nix repl` is an interactive Nix evaluator. It is underused as a debugging tool but is extremely powerful for inspecting flakes, derivations, and module configurations.

### Starting the REPL

```bash
# Blank REPL (uses nixpkgs from NIX_PATH if available)
nix repl

# Load a flake on startup (Nix ≥ 2.19.2; requires repl-flake feature)
nix repl .
nix repl github:NixOS/nixpkgs/nixos-unstable

# Start in debugger mode (enter sub-REPL on eval error)
nix repl --debugger
nix repl --debugger .

# Allow impure evaluation (access to env vars, currentSystem, currentTime)
nix repl --impure

# Load a legacy Nix file (not a flake)
nix repl --file '<nixpkgs>'
nix repl -f default.nix
```

### Special REPL Commands (`:?` to list all)

| Command | Effect |
|---------|--------|
| `:lf <url>` | Load a flake; binds `outputs`, `self`, `inputs` |
| `:l <file>` | Load a Nix file into scope (like `import`) |
| `:a <expr>` | Add attrs from an attrset into top-level scope |
| `:b <expr>` | Build a derivation; creates `./result` symlink |
| `:e <expr>` | Open expression's source in `$EDITOR` |
| `:p <expr>` | Pretty-print, fully forcing all lazy values (like `builtins.deepSeq`) |
| `:t <expr>` | Show Nix type: `int`, `string`, `set`, `list`, `lambda`, `bool`, `null`, `float`, `path`, `derivation` |
| `:doc <builtin>` | Show documentation for a builtin function |
| `:log <drv>` | Show build log for a derivation (if cached) |
| `:r` | Reload all loaded files |
| `:q` | Quit |
| `:paste` | Enter multi-line paste mode (terminated by `;;`) |

**`:lf .` — Load Flake:**
```
nix-repl> :lf .
Added 9 variables.

nix-repl> outputs.<TAB>    # tab-complete to explore
nix-repl> outputs.packages.x86_64-linux.default
«derivation /nix/store/...-bmci-0.1.0.drv»

nix-repl> inputs.nixpkgs.rev
"abc123..."
```

After `:lf .`, these names become available: `self`, `outputs`, `inputs`, and all flake output names directly (`packages`, `devShells`, `nixosConfigurations`, etc.).

**`:b` — Build in REPL:**
```
nix-repl> :b outputs.packages.x86_64-linux.default
This derivation produced the following outputs:
  out → /nix/store/...-bmci-0.1.0
```
Creates a `./result` symlink in `$PWD`. Useful for quick build tests without leaving the REPL.

**`:p` — Force and Print:**
```
nix-repl> :p { a = 1; b = [ 1 2 3 ]; }
{ a = 1; b = [ 1 2 3 ]; }

# WARNING: :p on a large attrset forces all lazy thunks — can be very slow or OOM
nix-repl> :p outputs.packages    # DO NOT do this on a large flake
```

**`:t` — Type Inspection:**
```
nix-repl> :t outputs.packages.x86_64-linux.default
"derivation"
nix-repl> :t outputs.packages
"set"
nix-repl> :t (x: x + 1)
"lambda"
nix-repl> :t null
"null"
```

**`:doc` — Builtin Documentation:**
```
nix-repl> :doc builtins.fetchGit
Synopsis: builtins.fetchGit args
Fetches a Git repository...
  url: URL of the repository
  rev: Git revision (SHA-1 hash)
  ...
```

**`:e` — Open Source in Editor:**
```
nix-repl> :e pkgs.hello
# Opens /nix/store/...-source/pkgs/applications/misc/hello/default.nix in $EDITOR
```
Requires `$EDITOR` to be set. Useful for reading nixpkgs package definitions without navigating the source tree.

---

### Practical Debugging Patterns

**Inspecting a NixOS configuration:**
```
nix-repl> :lf .
nix-repl> nixosConfig = outputs.nixosConfigurations.myhost
nix-repl> nixosConfig.config.services.nginx.enable
false
nix-repl> nixosConfig.config.environment.systemPackages
[ «derivation ...» «derivation ...» ... ]
nix-repl> :t nixosConfig.config.services.postgresql.settings
"set"
```

**Debugging an overlay:**
```
nix-repl> :lf .
nix-repl> pkgs = outputs.packages.x86_64-linux
nix-repl> pkgs.myPkg.meta.description
"..."
nix-repl> pkgs.myPkg.buildInputs
[ «derivation ...» ]

# Check override works:
nix-repl> pkgs.myPkg.override { enableFeature = true; }
«derivation /nix/store/...-myPkg-with-feature.drv»
```

**Exploring nixpkgs interactively:**
```
nix-repl> :lf nixpkgs
nix-repl> pkgs = legacyPackages.x86_64-linux
nix-repl> pkgs.hello.version
"2.12.1"
nix-repl> pkgs.hello.meta.license
{ fullName = "GNU General Public License v3.0 or later"; ... }
nix-repl> pkgs.hello.buildInputs
[ ]
```

**Module system debugging — finding where an option is defined:**
```
nix-repl> :lf .
nix-repl> cfg = outputs.nixosConfigurations.myhost
nix-repl> cfg.options.services.nginx.enable
{ _type = "option"; definitions = [ ... ]; declarations = [ ... ]; ... }
nix-repl> cfg.options.services.nginx.enable.declarations
[ { column = 7; file = "/nix/store/...-source/nixos/modules/services/web-servers/nginx/default.nix"; line = 42; } ]
```

**Checking a derivation's attributes without building:**
```
nix-repl> d = outputs.packages.x86_64-linux.default
nix-repl> d.name
"bmci-0.1.0"
nix-repl> d.drvPath
"/nix/store/xxxxxx-bmci-0.1.0.drv"
nix-repl> d.outPath
"/nix/store/yyyyyy-bmci-0.1.0"
nix-repl> d.src
«derivation /nix/store/...-source.drv»
```

---

### `--debugger` Mode

The most powerful and underused feature. When evaluation hits an error, instead of printing the error and exiting, `--debugger` drops into an interactive sub-REPL at the error site.

```bash
nix repl --debugger .
# or
nix eval --debugger .#packages.x86_64-linux.default
```

**Commands inside the debugger:**

| Command | Effect |
|---------|--------|
| `:env` | Show all bindings at current stack frame |
| `:bt` | Show full stack backtrace |
| `:up` | Move up one stack frame |
| `:down` | Move down one stack frame |
| `:q` | Exit debugger (abort evaluation) |
| (any Nix expression) | Evaluate in current scope |

**Example — debugging a module system error:**
```
error: The option 'services.myApp.port' does not exist.
nix debugger> :bt
#0  /nix/store/...-source/nixos/modules/system/activation/top-level.nix:42:12
#1  /nix/store/...-source/nixos/lib/eval-config.nix:88:5
nix debugger> config
{ services = { ... }; ... }
nix debugger> config.services.myApp
error: attribute 'myApp' missing
```

This is invaluable for deep module system errors where the default error message doesn't show enough context.

---

### Multi-line Input

```
# Method 1: backslash continuation
nix-repl> let x = 1; \
          y = 2; \
          in x + y
3

# Method 2: :paste mode
nix-repl> :paste
let
  x = 1;
  y = 2;
in x + y
;;
3
```

The `;;` terminates paste mode.

---

### Reload Workflow

```
nix-repl> :lf .          # initial load
# (edit flake.nix in editor)
nix-repl> :r             # reload all files
nix-repl> :lf .          # re-evaluate flake
```

Note: `:r` alone does not automatically re-load the flake. After `:r` you must call `:lf .` again. There is no `--watch` flag as of Nix 2.34.

---

### Custom REPL Scope from Flake

A useful pattern: expose a `repl` attrset from your flake that pre-populates the scope with useful values:

```nix
# In flake.nix outputs:
repl = {
  inherit (self) packages;
  pkgs = nixpkgs.legacyPackages.x86_64-linux;
  lib  = nixpkgs.lib;
  nixosConfig = self.nixosConfigurations.myhost;
};
```

```
nix-repl> :lf .
nix-repl> :a repl
nix-repl> nixosConfig.config.services.caddy.enable
true
nix-repl> lib.version
"24.11pre-git"
```

---

### Nix Version Notes for `nix repl`

- **Nix < 2.14:** `:lf` requires `--extra-experimental-features repl-flake`
- **Nix 2.14+:** `repl-flake` merged into `nix-command`; `:lf` works with `nix-command flakes` enabled
- **Nix 2.19.2+:** `nix repl .` (flake as argument) works without `:lf`
- **Nix 2.24+:** `:doc` expanded to non-builtin functions
- **Nix 2.30+:** `--eval-profiler flamegraph` works inside repl expressions
- `:log` requires the build log to be locally available (in `/nix/var/log/nix/drvs/`); it does not fetch logs from a remote builder

---

## Fetcher Quick Reference

| Function | Source | Hash mode | Notes |
|----------|--------|-----------|-------|
| `fetchurl` | Any URL | `flat` (file) | Foundation; all others build on it |
| `fetchzip` | Tarball/zip URL | `recursive` (NAR of unpacked) | `stripRoot = true` default |
| `fetchFromGitHub` | GitHub archive | `recursive` (NAR) | Uses `fetchzip`; fast; no history |
| `fetchFromGitHub` + `fetchSubmodules` | GitHub clone | `recursive` (NAR) | Uses `fetchgit`; slow |
| `fetchFromGitLab` | GitLab archive | `recursive` (NAR) | `domain =` for self-hosted |
| `fetchFromSourcehut` | Sourcehut archive | `recursive` (NAR) | `~owner` convention |
| `fetchgit` | Any git URL | `recursive` (NAR) | Full clone; submodules; LFS |
| `fetchsvn` | SVN URL | `recursive` (NAR) | Legacy VCS |
| `fetchhg` | Mercurial URL | `recursive` (NAR) | Legacy VCS |
| `fetchPypi` | PyPI | `flat` or `recursive` | `pname`+`version` auto-URL |
| `fetchCrate` | crates.io | `recursive` (NAR) | `pname`+`version` auto-URL |

## Substitution Function Quick Reference

| Function | Strict | Error on unknown | Null values |
|----------|--------|-----------------|-------------|
| `substitute` | Manual flags | Depends on `--replace-fail` | N/A |
| `substituteAll` | No | Silent skip | N/A |
| `substituteAllFiles` | No | Silent skip | N/A |
| `replaceVars` | Yes | Build failure | Skips check |

Sources:
- [pkgs/build-support/fetchurl/default.nix](https://github.com/NixOS/nixpkgs/blob/master/pkgs/build-support/fetchurl/default.nix)
- [pkgs/build-support/fetchgithub/default.nix](https://github.com/NixOS/nixpkgs/tree/master/pkgs/build-support/fetchgithub)
- [pkgs/build-support/fetchzip/default.nix](https://github.com/NixOS/nixpkgs/blob/master/pkgs/build-support/fetchzip/default.nix)
- [pkgs/build-support/fetchgit/default.nix](https://github.com/NixOS/nixpkgs/blob/master/pkgs/build-support/fetchgit/default.nix)
- [pkgs/build-support/trivial-builders/default.nix](https://github.com/NixOS/nixpkgs/blob/master/pkgs/build-support/trivial-builders/default.nix)
- [pkgs/build-support/substitute/substitute.nix](https://github.com/NixOS/nixpkgs/blob/master/pkgs/build-support/substitute/substitute.nix)
- [pkgs/build-support/replace-vars/replace-vars.nix](https://github.com/NixOS/nixpkgs/tree/master/pkgs/build-support/replace-vars)
- [pkgs/stdenv/generic/setup.sh](https://github.com/NixOS/nixpkgs/blob/master/pkgs/stdenv/generic/setup.sh)
- [pkgs/build-support/setup-hooks/make-wrapper.sh](https://github.com/NixOS/nixpkgs/blob/master/pkgs/build-support/setup-hooks/make-wrapper.sh)
- [Nixpkgs manual — Fetchers](https://ryantm.github.io/nixpkgs/builders/fetchers/)
- [nix repl manual — Nix 2.34](https://nix.dev/manual/nix/2.34/command-ref/new-cli/nix3-repl.html)
