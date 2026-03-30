# Nix Flakes: FODs, IFD, `nix shell`/`run`, narinfo, `callPackage`, Subflakes

Source-grounded research on fixed-output derivations, Import From Derivation, runtime commands, the binary cache wire format, `callPackage` mechanics, and monorepo patterns. All findings traced to primary sources (Nix source code, nixpkgs, official docs).

---

## Topic 1: Fixed-Output Derivations (FODs) In Depth

### What is a FOD?

A **fixed-output derivation** is a store derivation where the cryptographic hash of the single output is declared in advance via the `outputHash` attribute. From the Nix glossary (`doc/manual/source/glossary.md`):

> "A [store derivation] where a cryptographic hash of the [output] is determined in advance using the `outputHash` attribute, and where the `builder` executable has access to the network."

All three attributes must be set together: `outputHash`, `outputHashAlgo`, and `outputHashMode`. Any other combination of these is invalid.

A regular (input-addressed) derivation's output path is computed from a hash of all its inputs — source code, build scripts, dependencies. A FOD's output path is computed from the declared output hash itself, not the inputs. This means a FOD with a given `outputHash` always produces the same store path, regardless of what its builder actually does.

### Why FODs Are Allowed Network Access

This is implemented in `src/libstore/unix/build/linux-derivation-builder.cc`. The Linux build sandbox uses Linux network namespaces (`CLONE_NEWNET`) to isolate builders from the host network. The code conditionally adds `CLONE_NEWNET` only when `derivationType.isSandboxed()` is true:

```cpp
if (derivationType.isSandboxed())
    options.cloneFlags |= CLONE_NEWNET;
```

FODs return `false` from `isSandboxed()`, so `CLONE_NEWNET` is never added — the builder process inherits the host's network namespace and can make outbound connections. The rationale (from comments in the same file):

> "The private network namespace ensures that the builder cannot talk to the outside world (or vice versa). It only has a private loopback interface. (Fixed-output derivations are not run in a private network namespace to allow functions like fetchurl to work.)"

When a FOD's builder is in the network-enabled namespace, nss resolution is deliberately limited:

> "Only use nss functions to resolve hosts and services. Don't use it for anything else that may be configured for this system. This limits the potential impurities introduced in fixed-outputs."

The philosophical justification: since the output hash is predetermined, any impurity in the build environment is irrelevant — the final store object is verified against the declared hash. If the hash matches, the content is correct. If it doesn't, the build fails.

### `outputHashMode`: `"flat"` vs `"recursive"` / `"nar"`

From `doc/manual/source/language/advanced-attributes.md`:

- **`"flat"` (default)**: Hashes the single file directly. Use this for fetching a single file (e.g., a tarball, a single source file). The hash is computed over the raw bytes of the file.

- **`"recursive"` or `"nar"`**: Serializes the entire file system object as a NAR (Nix Archive) and hashes that. Use this when the output is a directory or when you need to preserve file permissions/executable bits on a single file. The alias `"nar"` was added in Nix 2.21; `"recursive"` has been supported since 2005.

- **`"text"`**: Experimental (`dynamic-derivations` feature). Used for derivation files stored in the Nix store.

- **`"git"`**: Experimental (`git-hashing` feature). Uses Git's tree hashing algorithm.

The practical implication: if you fetch a tarball and unpack it into a directory, use `outputHashMode = "recursive"`. If you fetch a file and keep it as-is, use the default `"flat"`.

### `outputHash` Format: SRI vs Legacy

`outputHash` accepts three formats:

1. **SRI format** (W3C Subresource Integrity): `"sha256-<base64>"` e.g., `"sha256-3XYHZANT6AFBV0BqegkAZHbba6oeDkIUCDwbATLMhAY="`. When using SRI, `outputHashAlgo` can be `null` because the algorithm is embedded.

2. **Nix32 encoding**: Nix's variant of base-32. The old-style format used as `outputHash = "sha256:<nix32-hash>"` with a separate `outputHashAlgo = "sha256"`.

3. **Hexadecimal**: raw hex string, requires `outputHashAlgo` set.

In modern nixpkgs (`pkgs/build-support/fetchurl/default.nix`), the preferred interface is the `hash` parameter which accepts SRI format directly. The logic normalizes to `outputHashAlgo = null` when the hash is SRI-formatted.

### Hash Mismatch Error

From `src/libstore/build/derivation-check.cc`, the exact error thrown on a hash mismatch is a `BuildError` with this format string:

```
"hash mismatch in fixed-output derivation '%s':\n  specified: %s\n     got:    %s"
```

Where both `specified` and `got` are rendered in SRI format (`HashFormat::SRI`). Example output:

```
error: hash mismatch in fixed-output derivation '/nix/store/...-source.drv':
  specified: sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
       got: sha256-3XYHZANT6AFBV0BqegkAZHbba6oeDkIUCDwbATLMhAY=
```

A FOD that references other store paths also fails with:
```
"fixed-output derivations must not reference store paths: '%s' references %d distinct paths, e.g. '%s'"
```

### `impureEnvVars`

From `doc/manual/source/language/advanced-attributes.md`:

> "This attribute allows you to specify a list of environment variables that should be passed from the environment of the calling user to the builder. Usually, the environment is cleared completely when the builder is executed, but with this attribute you can allow specific environment variables to be passed unmodified."

**Only valid in fixed-output derivations.** It is silently ignored for all other derivations.

**Critical daemon caveat**: When building via the Nix daemon, `impureEnvVars` reads from the daemon's environment, not the user's. Users invoking `nix-build` directly (no daemon) get their own environment variables.

From `src/libstore/unix/build/derivation-builder.cc`:

```cpp
/* *Only* if this is a fixed-output derivation, propagate the
   values of the environment variables specified in the
   `impureEnvVars' attribute to the builder. */
if (derivationType.isFixed())
    // ... propagate impureEnvVars
```

### `http_proxy` / `NIX_CURL_FLAGS` for Corporate Proxies

The `impureEnvVars` mechanism is the primary proxy configuration route. In `pkgs/build-support/fetchurl/default.nix`, `impureEnvVars` is set to `lib.fetchers.proxyImpureEnvVars` plus additional flags. From `lib/fetchers.nix`:

```nix
proxyImpureEnvVars = [
  # We borrow these environment variables from the caller to allow
  # easy proxy configuration. This is impure, but a fixed-output
  # derivation like fetchurl is allowed to do so since its result is
  # by definition pure.
  "http_proxy"
  "https_proxy"
  "ftp_proxy"
  "all_proxy"
  "no_proxy"
  "HTTP_PROXY"
  "HTTPS_PROXY"
  "FTP_PROXY"
  "ALL_PROXY"
  "NO_PROXY"
  # https proxies typically need to inject custom root CAs too
  "NIX_SSL_CERT_FILE"
];
```

The `fetchurl` derivation adds three more:

```nix
impureEnvVars = lib.fetchers.proxyImpureEnvVars ++ [
  "NIX_CURL_FLAGS"       # additional curl options
  "NIX_HASHED_MIRRORS"   # override hashed mirrors
  "NIX_CONNECT_TIMEOUT"  # timeout for hashed mirrors
];
```

So `export http_proxy="http://proxy:3128"` in the calling environment (or daemon environment) propagates to all `fetchurl`-based FODs.

### `nix flake prefetch` vs `nix-prefetch-url`

**`nix flake prefetch`** (from `src/nix/flake-prefetch.md`):
- Downloads a source tree denoted by a flake reference
- Does NOT require the target to contain `flake.nix` — it can fetch any source tree
- Computes the NAR hash (SHA256 of the NAR serialization of the unpacked tree)
- Outputs: store path + `narHash` in SRI format
- The `narHash` is what goes into `flake.lock` and into `narHash =` inputs
- With `--json`: `{"hash":"sha256-...","storePath":"/nix/store/..."}`
- Example output: `hash 'sha256-3XYHZANT6AFBV0BqegkAZHbba6oeDkIUCDwbATLMhAY='`

**`nix-prefetch-url`** (legacy command):
- Downloads a single file referenced by URL
- Hash defaults to SHA256, base-32 encoded (unless `--type md5` uses base-16)
- Options: `--type <algo>`, `--print-path`, `--unpack`, `--executable`, `--name`
- `--unpack` unpacks tarballs/zips before hashing — this is what produces a hash suitable for `outputHashMode = "recursive"` FODs
- Without `--unpack`: produces a flat file hash for `outputHashMode = "flat"`
- Default output: just the hash on stdout; `--print-path` adds the store path on a second line
- **Primary use case**: get the `sha256` for `pkgs.fetchurl { url = ...; sha256 = "..."; }`

The key difference: `nix flake prefetch` produces a `narHash` for use in `flake.lock` and flake input `narHash =` parameters. `nix-prefetch-url` produces an `outputHash` for use inside `fetchurl` / `fetchzip` / `mkDerivation` derivation expressions.

### The `fod` Helper Pattern in nixpkgs

nixpkgs does not export a function literally named `fod`. Instead, it provides a family of fetchers built on `pkgs/build-support/fetchurl/default.nix` which is itself a FOD wrapper. The pattern is:

```nix
fetchurl {
  url = "https://example.com/foo-1.0.tar.gz";
  hash = "sha256-<SRI-hash>";  # preferred: SRI format, algo null
  # OR legacy:
  sha256 = "<nix32-or-hex>";
}
```

The `fetchurl` function uses `lib.extendMkDerivation` to build a `stdenvNoCC.mkDerivation` call with the hash attributes, `preferLocalBuild = true`, and `impureEnvVars` set. Other fetchers (`fetchzip`, `fetchgit`, `fetchFromGitHub`) delegate to `fetchurl` for the actual download step.

---

## Topic 2: Import From Derivation (IFD)

### Exact Definition

From `doc/manual/source/language/import-from-derivation.md`:

> "The value of a Nix expression can depend on the contents of a store object."

IFD occurs when an expression that evaluates to a **store path** (i.e., a derivation output) is passed to any of these filesystem-reading builtins:

- `import expr`
- `builtins.readFile expr`
- `builtins.readFileType expr`
- `builtins.readDir expr`
- `builtins.pathExists expr`
- `builtins.filterSource f expr`
- `builtins.path { path = expr; }`
- `builtins.hashFile t expr`
- `builtins.scopedImport x drv`

### Why IFD Breaks Eval/Build Separation

Nix normally separates two phases:
1. **Evaluation**: The Nix language evaluator runs, producing a complete build plan (a set of store derivations).
2. **Realisation**: The build plan is executed, potentially in parallel, building all derivations.

With IFD, this separation collapses. From the manual:

> "When the store path needs to be accessed, evaluation will be paused, the corresponding store object realised, and then evaluation resumed."

The evaluator is single-threaded and sequential. It discovers one store path at a time. When it hits an IFD, it must block and wait for that derivation to build before it can continue evaluating. This means:

> "Evaluation can only finish when all required store objects are realised. Since the Nix language evaluator is sequential, it only finds store paths one at a time. While realisation is always parallel, in this case it cannot be done for all required store paths at once, and is therefore much slower."

### Why IFD Is Disabled in Flakes (Pure Eval)

Flakes use "pure evaluation" mode by default. In pure eval, IFD is disabled because it violates hermetic evaluation semantics — the result of evaluation depends on the results of builds, which are side effects. Pure eval requires that evaluation be deterministic and not trigger side effects.

The `allow-import-from-derivation` setting in `nix.conf` controls this:
- **Default**: `true` (IFD permitted in non-flake evaluation)
- **Flakes**: effectively `false` during pure eval

From the conf-file documentation:
> "By default, Nix allows Import from Derivation. With this option set to `false`, Nix will throw an error when evaluating an expression that uses this feature."

When set to `false`:
> "evaluation is complete and Nix can produce a build plan before starting any realisation" — enabling full parallel realisation of all dependencies.

### Real Use Cases That Require IFD

1. **`gomod2nix`**: Generates a `gomod2nix.toml`-derived Nix expression at build time; the consumer `import`s the output. This is a classic IFD pattern.

2. **`dream2nix`**: Generates lock file translations (package-lock.json → Nix) as derivation outputs and then imports them.

3. **Some NixOS module generators**: Modules that generate Nix expressions from YAML/JSON config files and then import the result.

4. **`npmlock2nix`**, **`node2nix`**: npm lock file translation tools that produce Nix expressions as build outputs.

5. **Dynamic dependency resolution**: When the set of packages to build cannot be determined at evaluation time without first running a tool.

### Performance Implications

The sequential-discovery problem: suppose you have a chain of 10 IFDs where each depends on the previous. You must wait for build 1 to complete, then evaluate to find build 2, wait for it, etc. With `allow-import-from-derivation = false`, Nix would instead compute all 10 derivations upfront and build them in parallel.

IFD also prevents remote evaluation caching — the eval cannot be cached separately from the build.

### The Practical Example (from docs)

```nix
let
  drv = derivation {
    name = "hello";
    builder = "/bin/sh";
    args = [ "-c" "echo -n hello > $out" ];
    system = builtins.currentSystem;
  };
in "${builtins.readFile drv} world"
```

`builtins.readFile drv` triggers IFD: evaluation pauses, `drv` is built (producing the string "hello" in the store), then evaluation resumes to produce `"hello world"`.

---

## Topic 3: `nix shell` vs `nix develop` vs `nix run`

### `nix shell` — Exact Behavior

From `src/nix/shell.md` (source of truth):

> "`nix shell` runs a command in an environment in which the `$PATH` variable provides the specified installables. If no command is specified, it starts the default shell of your user account specified by `$SHELL`."

**What it does to PATH**: Prepends the `bin/` directories of all specified packages to `$PATH`. Nothing else. It does NOT source stdenv setup, does NOT run `shellHook`, does NOT set build-phase environment variables like `$src`, `$buildInputs`, etc.

**Multiple packages**: Space-separate installables:
```console
nix shell nixpkgs#git nixpkgs#jq
```

**`--command` flag**: Runs a command inside the environment:
```console
nix shell nixpkgs#hello --command hello --greeting 'Hi everybody!'
```

**Use as `#!`-interpreter**: A key non-obvious feature — `nix` can serve as a script interpreter:
```python
#! /usr/bin/env nix
#! nix shell github:tomberek/-#python3With.prettytable --command python
```
Multiple `#! nix` lines are merged. Cannot use `#! /usr/bin/env nix shell -i ...` because most OSes allow only one argument on `#!` lines.

**Chroot store example**:
```console
nix shell --store ~/my-nix nixpkgs#hello nixpkgs#bashInteractive --command bash
```
Must explicitly include `bashInteractive` since `/bin/bash` won't exist in the chroot store.

### `nix develop` — Exact Behavior

From `src/nix/develop.md`:

> "`nix develop` starts a bash shell that provides an interactive build environment nearly identical to what Nix would use to build the installable."

**How it works**: Nix builds a modified version of the target derivation that records the environment initialized by `stdenv` and then exits. This captured environment (all `buildInputs` added to `PATH`/`PKG_CONFIG_PATH`/`CFLAGS`/etc., all `nativeBuildInputs` configured, etc.) becomes the interactive shell context.

**`shellHook`**: If the derivation defines a `shellHook` attribute (typically via `mkShell`), it is sourced after stdenv setup. From legacy `nix-shell` docs:
> "If the derivation defines the variable `shellHook`, it will be run after `$stdenv/setup` has been sourced."

This is the mechanism used in `flake.nix` devShells:
```nix
devShells.default = pkgs.mkShell {
  buildInputs = [ pkgs.go pkgs.gopls ];
  shellHook = ''
    export GOPATH="$HOME/go"
    echo "Go dev shell ready"
  '';
};
```

**Default flake output resolution**:
- No attribute: tries `devShells.<system>.default`, then `packages.<system>.default`
- Named attribute: tries `devShells.<system>.<name>`, `packages.<system>.<name>`, `legacyPackages.<system>.<name>`

**Build phases**: Can run specific stdenv phases directly:
```console
nix develop --unpack    # runs unpackPhase
nix develop --configure # runs configurePhase
nix develop --build     # runs buildPhase
```

**Prompt customization**: `bash-prompt`, `bash-prompt-prefix`, `bash-prompt-suffix` settings in `nix.conf` or flake `nixConfig`.

### Key Differences: `nix shell` vs `nix develop`

| Aspect | `nix shell` | `nix develop` |
|--------|------------|---------------|
| Input | Any installable (package) | Derivation with build environment |
| PATH modification | Adds `bin/` of specified packages | Sets up full stdenv environment including all `buildInputs`, `nativeBuildInputs` |
| `shellHook` | No | Yes (if derivation defines it) |
| Build variables | No (`$src`, `$configureFlags`, etc. absent) | Yes (all derivation attrs as env vars) |
| stdenv phases | Not available | Available (`configurePhase`, `buildPhase`, etc.) |
| Shell | Your `$SHELL` | Always bash |
| Typical use | "I want tool X available" | "I want to work on/build package X" |
| Flake output | Any `packages.*` | `devShells.*` or `packages.*` |

### `nix run` — Binary Resolution

From `src/nix/run.md`, the exact algorithm for finding the executable:

> "If installable evaluates to a derivation, it will try to execute the program `<out>/bin/<name>`, where `out` is the primary output store path of the derivation, and `name` is the first of the following that exists:
> 1. The `meta.mainProgram` attribute of the derivation.
> 2. The `pname` attribute of the derivation.
> 3. The name part of the value of the `name` attribute of the derivation."

Example: if `name = "hello-1.10"`, runs `$out/bin/hello`.

**Apps**: A flake can expose explicit `apps` outputs:
```nix
apps.x86_64-linux.blender_2_79 = {
  type = "app";  # required
  program = "${self.packages.x86_64-linux.blender_2_79}/bin/blender";  # required, must be Nix store path
  meta.description = "...";  # optional
};
```

**Passing args with `--`**:
```console
nix run nixpkgs#vim -- --help
```
Everything after `--` is passed to the executed program. Note: the first positional argument is always the installable even after `--`, so `nix run . -- arg1 arg2` explicitly specifies the installable as `.`.

**Flake output resolution**: Without explicit name: tries `apps.<system>.default`, then `packages.<system>.default`.

### `nix shell` vs `nix-shell` (Legacy)

`nix-shell` (legacy) is **distinct** from `nix shell`. From the docs:
> "This man page describes the command `nix-shell`, which is distinct from `nix shell`."

Key differences:

| Aspect | `nix-shell` (legacy) | `nix shell` (new) |
|--------|---------------------|------------------|
| Default file | `shell.nix` or `default.nix` | Flake output |
| `shellHook` | Yes | No |
| `--pure` flag | Yes (clears environment except HOME, USER, DISPLAY) | No equivalent |
| `-p`/`--packages` flag | `nix-shell -p git jq` (uses nixpkgs from NIX_PATH) | `nix shell nixpkgs#git nixpkgs#jq` |
| `-i interpreter` | Yes (for `#!`-scripts) | Via `--command` |
| stdenv setup | Yes (sources `$stdenv/setup`) | No |
| Shell | `NIX_BUILD_SHELL` env var, defaults to `bashInteractive` from nixpkgs | Your `$SHELL` |

---

## Topic 4: Binary Cache Narinfo Format

### The `.narinfo` File Format

The narinfo file is a text file with a `Key: value\n` line format. The filename is `<hashPart>.narinfo` where `hashPart` is the first 32 characters of the store path (the base-32 hash component).

URL construction: for a store path `/nix/store/sl5vvk8mb4ma1sjyy03kwpvkz50hd22d-source`, Nix fetches `https://cache.nixos.org/sl5vvk8mb4ma1sjyy03kwpvkz50hd22d.narinfo`.

From `src/libstore/nar-info.cc` (the parser), the fields are:

| Field | Description | Required |
|-------|-------------|----------|
| `StorePath` | Full store path: `/nix/store/<hash>-<name>` | Yes |
| `URL` | Relative or absolute URL to the compressed NAR file | Yes |
| `Compression` | Compression algorithm: `bzip2`, `xz`, `zstd`, `br`, `none`. Defaults to `bzip2` if empty | Yes (implicit) |
| `FileHash` | Hash of the compressed NAR file (the download). Format: `sha256:<nix32>` | No |
| `FileSize` | Size of the compressed NAR in bytes | No |
| `NarHash` | Hash of the uncompressed NAR. Format: `sha256:<nix32>`. **Required** | Yes |
| `NarSize` | Size of the uncompressed NAR in bytes. **Required**, must be non-zero | Yes |
| `References` | Space-separated list of store path basenames (no `/nix/store/` prefix) | No |
| `Deriver` | Basename of the `.drv` file that produced this path. `"unknown-deriver"` if unknown | No |
| `Sig` | One or more signatures. Format: `<key-name>:<base64-sig>`. Multiple `Sig:` lines allowed | No |
| `CA` | Content address for content-addressed paths. Format: `<method>:<hash>` | No |

From `nar-info.cc` `to_string()`:
```
assert(fileHash && fileHash->algo == HashAlgorithm::SHA256);  // FileHash always SHA256
assert(narHash.algo == HashAlgorithm::SHA256);                  // NarHash always SHA256
```

Both `FileHash` and `NarHash` are serialized in nix32 format in the text file.

### Example `.narinfo`

```
StorePath: /nix/store/sl5vvk8mb4ma1sjyy03kwpvkz50hd22d-source
URL: nar/1fzam4dpyniqm4k5lzqnasrmn6adijmxfh5z8fmv37sjzr0wr90d.nar.xz
Compression: xz
FileHash: sha256:1fzam4dpyniqm4k5lzqnasrmn6adijmxfh5z8fmv37sjzr0wr90d
FileSize: 1234567
NarHash: sha256:3xyhzant6afbv0bqegkazHbba6oeDkIUCDwbATLMhAY
NarSize: 9876543
References: glibc-2.35 openssl-3.0.1
Deriver: xxxxxx-source.drv
Sig: cache.nixos.org-1:abc123...
```

### Signature Verification

Signatures use **Ed25519** via `libsodium`. From `src/libutil/signature/local-keys.cc`:

The signature format in the `Sig:` field is:
```
<key-name>:<base64-encoded-raw-signature-bytes>
```

Where `<key-name>` is the name of the signing key (e.g., `cache.nixos.org-1`).

The **fingerprint** (the data that is signed) is computed in `src/libstore/path-info.cc`:

```cpp
std::string ValidPathInfo::fingerprint(const StoreDirConfig & store) const
{
    return "1;" + store.printStorePath(path) + ";"
           + narHash.to_string(HashFormat::Nix32, true) + ";"
           + std::to_string(narSize) + ";"
           + concatStringsSep(",", store.printStorePathSet(references));
}
```

Format: `1;<storePath>;<narHash-nix32>;<narSize>;<comma-separated-references>`

Example:
```
1;/nix/store/sl5vvk8mb4ma1sjyy03kwpvkz50hd22d-source;sha256:3xyz...;9876543;/nix/store/abc-glibc-2.35,/nix/store/def-openssl-3.0.1
```

The signing uses `crypto_sign_detached` (libsodium Ed25519):

```cpp
Signature SecretKey::signDetached(std::string_view data) const {
    unsigned char sig[crypto_sign_BYTES];
    unsigned long long sigLen;
    crypto_sign_detached(sig, &sigLen, (unsigned char*)data.data(), data.size(), (unsigned char*)key.data());
    return Signature{ .keyName = name, .sig = std::string((char*)sig, sigLen) };
}
```

Verification checks that `sig.keyName == public_key.name` first, then verifies the signature bytes using `crypto_sign_verify_detached`.

**Important**: The signature covers the NarHash (content) and References. It does NOT cover the URL, compression method, FileHash, or FileSize — those can change (e.g., re-compression) without invalidating the signature.

### `.nar` Compression Formats

From `src/libstore/include/nix/store/http-binary-cache-store.hh` and the `BinaryCacheStoreConfig`:

- `xz` — default compression in nixpkgs's cache. Extension: `.nar.xz`
- `bzip2` — original default, still widely supported. Extension: `.nar.bz2`
- `gzip` — good browser compatibility. Extension: `.nar.gz`
- `zstd` — fast decompression, growing adoption. Extension: `.nar.zst`
- `none` — uncompressed. Extension: `.nar`

The `Compression:` field in narinfo identifies what's used. The `URL:` field typically contains the compressed file's path.

### `nix-cache-info` File

Located at the root of every binary cache: `https://cache.nixos.org/nix-cache-info`

From `doc/manual/source/protocols/nix-cache-info.md`:

**MIME type**: `text/x-nix-cache-info`

**Format**: Line-based `Key: value`. Leading/trailing whitespace trimmed. Lines without `:` ignored. Unknown keys silently ignored.

Fields:

| Field | Description |
|-------|-------------|
| `StoreDir` | Store directory path this cache was built for (e.g., `/nix/store`). Nix verifies it matches the client's store dir or errors: `"binary cache '...' is for Nix stores with prefix '/nix/store', not '/home/user/nix/store'"` |
| `WantMassQuery` | `1` or `0`. Whether Nix can efficiently query this cache for multiple paths simultaneously. Sets default for `want-mass-query` store parameter |
| `Priority` | Integer. Lower value = higher priority. Sets default for `priority` store parameter. cache.nixos.org uses 40; a typical self-hosted cache uses 30 to take priority |

Example:
```
StoreDir: /nix/store
WantMassQuery: 1
Priority: 30
```

**Caching**: Nix caches `nix-cache-info` in the local SQLite cache (`NarInfoDiskCache`) with a 7-day TTL.

**Priority semantics** (from HTTP Binary Cache Store config): "A lower value means a higher priority." When multiple substituters are configured, Nix queries them in priority order. cache.nixos.org uses priority 40 by default. A private cache with priority 30 will be checked first.

---

## Topic 5: `callPackage` Internals

### What `callPackage` Does

From `lib/customisation.nix` in nixpkgs:

```
callPackageWith :: AttrSet -> ((AttrSet -> a) | Path) -> AttrSet -> a
```

`callPackage` is `callPackageWith pkgs` — a partially applied version that uses the current package set as the auto-argument source.

The implementation:

```nix
callPackageWith = autoArgs: fn: args:
  let
    f = if isFunction fn then fn else import fn;   # (1) import file if path given
    fargs = functionArgs f;                          # (2) introspect function args
    allArgs = intersectAttrs fargs autoArgs // args; # (3) auto-fill + explicit override
  in
  if missingArgs == { } then
    makeOverridable f allArgs
  else
    abort "lib.customisation.callPackageWith: ${error}";
```

Step by step:
1. If `fn` is a path (e.g., `./foo.nix`), `import` it to get the function.
2. `functionArgs f` returns the function's argument set with `true`/`false` indicating whether each has a default.
3. `intersectAttrs fargs autoArgs` extracts only those attrs from `autoArgs` that the function actually needs.
4. `// args` applies explicit overrides (the third argument to `callPackage`).
5. Wraps with `makeOverridable` so the result has an `.override` function.

### `builtins.functionArgs`

From the Nix manual:

> "Return a set containing the names of the formal arguments expected by the function `f`. The value of each attribute is a Boolean denoting whether the corresponding argument has a default value."

Example: for `{ x, y ? 123 }: ...`, returns `{ x = false; y = true; }`.

**Critical non-obvious behavior with `...`**: For a function `{ x, y, ... }: ...`, `functionArgs` returns `{ x = false; y = false; }` — it returns only the named arguments, NOT the variadic rest. The `...` allows additional arguments to be passed without error, but they don't appear in `functionArgs`.

**Consequence for `callPackage`**: When a function uses `...`, `callPackage` passes only the named args it recognizes PLUS any explicit overrides in `args`. Any additional attrs from `autoArgs` are ignored (they'd cause an error without `...`; with `...` they'd just be unrecognized). The `...` pattern is commonly used in nixpkgs for forward compatibility.

**Plain lambdas**: `functionArgs (x: ...)` returns `{ }` — plain lambda args are NOT tracked.

### `callPackage` vs `import`

`callPackage ./foo.nix { }` differs from `import ./foo.nix pkgs` in three ways:

1. **Selective argument passing**: `callPackage` passes only the arguments the function declares; `import ./foo.nix pkgs` passes the entire `pkgs` attrset to the function. The function must accept it (e.g., via `@pkgs` or `pkgs@{ ... }`).

2. **`makeOverridable` wrapping**: `callPackage` wraps the result in `makeOverridable`, adding `.override` and `.overrideDerivation` to the returned derivation. `import ./foo.nix pkgs` does not.

3. **Missing argument detection**: `callPackage` uses `abort` (uncatchable) if a required argument is missing, with a helpful error message including typo suggestions. `import` passes whatever you give it.

### `makeOverridable`

From `lib/customisation.nix`:

```nix
makeOverridable = f: ...
```

Takes a function `f :: AttrSet -> a` and returns a version that, when called, adds `.override` to the result (if the result is an attrset or derivation).

```nix
nix-repl> x = {a, b}: { result = a + b; }
nix-repl> y = lib.makeOverridable x { a = 1; b = 2; }
nix-repl> y
{ override = «lambda»; overrideDerivation = «lambda»; result = 3; }
nix-repl> y.override { a = 10; }
{ override = «lambda»; overrideDerivation = «lambda»; result = 12; }
```

`.override` takes a new attrset (or function from old args to new args) and re-calls `f` with the merged args. It preserves the `override` chain recursively — `y.override { a = 10; }.override { b = 5; }` works.

### `override` vs `overrideAttrs`

- **`.override`**: Re-calls the original package function with different arguments. Changes are at the function-call level. Example: `pkgs.hello.override { stdenv = pkgs.clangStdenv; }`.

- **`.overrideAttrs`**: Modifies the attrset passed to `stdenv.mkDerivation` directly. Takes a function `old: { ... }` and merges the result with the old attrs. Example:
  ```nix
  pkgs.hello.overrideAttrs (old: {
    src = ./my-patched-source;
    patches = old.patches ++ [ ./my.patch ];
  })
  ```

`overrideAttrs` is preferred for most use cases. `override` is used when you need to change parameters that affect which derivation is constructed (e.g., switching the standard environment).

From `lib/customisation.nix`, `makeOverridable` also wires `.overrideAttrs` back through `makeOverridable` so that chained overrides remain consistent:

```nix
${if result ? overrideAttrs then "overrideAttrs" else null} =
  fdrv: overrideResult (x: x.overrideAttrs fdrv);
```

### `makeScope` and Package Scopes

```
makeScope :: (AttrSet -> callPackage) -> (AttrSet -> AttrSet) -> Scope
```

Used to create isolated package sets where packages can depend on each other via `self`. Implementation:

```nix
makeScope = newScope: f:
  let
    self = {
      callPackage = self.newScope { };
    }
    // f self
    // {
      newScope = scope: newScope (self // scope);
      overrideScope = g: makeScope newScope (extends g f);
      packages = f;
    };
  in
  self;
```

The scope's `callPackage` uses `self` as the auto-argument source — so packages in the scope automatically see each other as dependencies.

**Example** (from nixpkgs docs):
```nix
pkgs.lib.makeScope pkgs.newScope (self: {
  foo = self.callPackage ./foo.nix { };
  bar = self.callPackage ./bar.nix { };  # bar can depend on foo automatically
})
```

`overrideScope` uses `extends` (fixpoint) to apply overlays, ensuring that a changed package propagates to all consumers within the scope.

**Real use in nixpkgs**: `pythonPackages = python.pkgs`, `nodePackages`, `perlPackages`, etc. are all scopes.

---

## Topic 6: Monorepo / Subflake Patterns with `dir=`

### How `dir=` Works

From `src/nix/flake.md`:

> "`dir`: The subdirectory of the flake in which `flake.nix` is located. This parameter enables having multiple flakes in a repository or tarball. The default is the root directory of the flake."

URL-like examples:
```
github:edolstra/nix-warez?dir=blender
git+https://example.org/my/repo?dir=flake1
```

In `flake.nix` inputs:
```nix
inputs.sub = {
  url = "github:owner/monorepo?dir=packages/sub";
  # OR for same-repo reference:
  url = "path:./packages/sub";  # relative path
};
```

### Lock File Format for Same-Repo Subflakes

When a flake references another flake in the same repo using a relative path, the lock file node looks like:

```json
{
  "nodes": {
    "root": {
      "inputs": { "sub": "sub" },
      "locked": { ... }
    },
    "sub": {
      "locked": {
        "dir": "packages/sub",
        "lastModified": 1234567890,
        "narHash": "sha256-...",
        "path": "/nix/store/...",
        "type": "path"
      },
      "original": {
        "dir": "packages/sub",
        "path": "./packages/sub",
        "type": "path"
      }
    }
  },
  "root": "root",
  "version": 7
}
```

For GitHub-hosted monorepos, the locked node would use `type: "github"` with `dir:` preserved:
```json
{
  "locked": {
    "dir": "packages/sub",
    "owner": "owner",
    "repo": "monorepo",
    "rev": "abc123...",
    "type": "github"
  }
}
```

The entire repo is fetched once (one NAR download), but the `dir` field tells Nix which subdirectory contains `flake.nix`. This means multiple `dir=` inputs from the same repo revision share the same source fetch.

### `self` Reference in Subflakes

Within a subflake's `flake.nix`, `self` refers to that specific subflake's outputs and source tree — specifically the subdirectory, NOT the root of the repo. `self.outPath` points to the subdirectory's path in the Nix store (or the full repo root if the subflake was fetched independently).

This can be surprising: if you have `repo/flakeA/flake.nix` referencing `self`, `self.outPath` is the path to the `flakeA/` directory as it was fetched (which may be the full repo root if using a git fetcher with `dir=`).

### The `inherit (self) outPath` Pattern

When sharing code between a root flake and subflakes, a pattern is:

```nix
# root flake.nix
inputs.sub.url = "path:./sub";

outputs = { self, nixpkgs, sub }: {
  # Pass root flake's source to sub's build
  packages.x86_64-linux.combined = sub.packages.x86_64-linux.default.override {
    rootSrc = self.outPath;  # path to the whole repo
  };
};
```

Or in the subflake:
```nix
# sub/flake.nix
inputs.root = { url = "path:../"; };

outputs = { self, root, ... }: {
  packages.x86_64-linux.default = pkgs.mkDerivation {
    src = root;  # use root repo as source
  };
};
```

### Tradeoffs: Monorepo-with-Subflakes vs Single Flake

**Single root flake with multiple outputs** (simpler, recommended for most cases):
- One `flake.lock` for the entire repo
- All packages share the same nixpkgs pin
- Simpler evaluation: no IFD concerns, no inter-flake input dependencies
- `nix build .#packageA .#packageB` works naturally
- Cannot compose independently — updating one output updates all

**Monorepo with subflakes via `dir=`**:
- Each subflake has independent inputs and lock file (if pinned independently)
- Subflakes can be used independently: `nix build github:owner/monorepo?dir=sub`
- Different subflakes can pin different nixpkgs versions
- More complex: root flake must explicitly import/use subflake outputs
- Lock file management becomes complex: multiple `flake.lock` files or path inputs

**Practical recommendation**: Use a single flake with multiple outputs for packages within the same project. Use `dir=`-based subflakes only when components need to be independently versionable and usable as standalone flakes.

### Path Inputs vs `dir=` GitHub Inputs

For a monorepo in local development, use path inputs:
```nix
inputs.sub.url = "path:./packages/sub";
```
This is `path:`-typed, does not require the sub to be independently fetchable from GitHub.

For a published monorepo on GitHub, use:
```nix
inputs.sub.url = "github:owner/repo?dir=packages/sub";
```
This fetches the whole repo once and extracts the subdirectory.

**Shallow clone optimization**: `git+https://github.com/...?shallow=1` with `dir=` still works and fetches only the needed commit depth, which is useful for large monorepos.

---

## Sources

- `NixOS/nix` repo (master branch):
  - `doc/manual/source/language/advanced-attributes.md` — FOD attributes
  - `doc/manual/source/glossary.md` — FOD definition with network access note
  - `doc/manual/source/language/import-from-derivation.md` — IFD documentation
  - `doc/manual/source/protocols/nix-cache-info.md` — nix-cache-info format
  - `src/libstore/nar-info.cc` — narinfo parser/serializer
  - `src/libstore/path-info.cc` — fingerprint() for signatures
  - `src/libutil/signature/local-keys.cc` — Ed25519 sign/verify
  - `src/libutil/include/nix/util/signature/local-keys.hh` — signature struct
  - `src/libstore/unix/build/linux-derivation-builder.cc` — FOD sandbox exception (CLONE_NEWNET)
  - `src/libstore/unix/build/derivation-builder.cc` — impureEnvVars propagation
  - `src/libstore/binary-cache-store.cc` — narInfoFileFor() URL construction
  - `src/libstore/include/nix/store/http-binary-cache-store.hh` — compression settings
  - `src/nix/shell.md` — nix shell documentation
  - `src/nix/develop.md` — nix develop documentation
  - `src/nix/run.md` — nix run documentation
  - `doc/manual/source/command-ref/nix-shell.md` — legacy nix-shell documentation
  - `src/nix/flake.md` — flake references, dir= parameter
  - `src/nix/flake-prefetch.md` — nix flake prefetch documentation
  - `src/libstore/build/derivation-check.cc` — FOD hash mismatch error format

- `NixOS/nixpkgs` repo (master branch):
  - `pkgs/build-support/fetchurl/default.nix` — fetchurl FOD implementation
  - `lib/customisation.nix` — callPackageWith, makeOverridable, makeScope
  - `lib/fetchers.nix` — proxyImpureEnvVars

- `nix.dev` official documentation:
  - Advanced attributes page
  - IFD best practices
  - nix run documentation
  - nix develop documentation
  - Flake lock file format

- Nix configuration (`nix.conf`): `allow-import-from-derivation` default and description
