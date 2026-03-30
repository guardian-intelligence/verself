# Self-Hosted Binary Caches and Flake Input Fetcher Types

Source-grounded research on the Nix binary cache ecosystem (nix-serve, nix-serve-ng, harmonia, attic, cachix, S3, post-build-hook, signing) and the complete flake input URL reference.

---

## Part 1: Binary Cache Fundamentals

A Nix binary cache is an HTTP server that responds to the Nix binary cache protocol: clients request `.narinfo` files by store hash prefix, then fetch `.nar` (or `.nar.xz` / `.nar.zst`) archives. The protocol is defined in `src/libstore/binary-cache-store.cc`. Any store path that has been built locally can be uploaded to a cache; clients substitute (download pre-built outputs) instead of building from source.

The root of every binary cache is a `nix-cache-info` file:

```
StoreDir: /nix/store
WantMassQuery: 1
Priority: 40
```

- `StoreDir`: must match the client's store prefix or Nix rejects the cache
- `WantMassQuery`: `1` means the client can query for multiple paths in one batch
- `Priority`: lower number = higher substitution priority (cache.nixos.org uses 40)

Signing is enforced at the `.narinfo` level. Each `.narinfo` has one or more `Sig:` fields. The client verifies against `trusted-public-keys` in `nix.conf`. Without a valid signature, substitution is rejected (unless `require-sigs = false`, a security downgrade).

---

## Part 2: Signing Key Management

### Generating a signing key pair

```bash
nix-store --generate-binary-cache-key <name> secret.pem public.pem
```

`<name>` is a human-readable identifier embedded in every signature, by convention `hostname-N` (e.g. `cache.example.com-1`). The number suffix allows key rotation without renaming.

Key format is Ed25519, encoded as `<name>:<base64(32-byte-key)>`. The full signature in `.narinfo`:
```
Sig: cache.example.com-1:base64encodedEd25519Signature==
```

Source: `src/libutil/signature/local-keys.cc` in NixOS/nix. The signed message is a deterministic serialization of `{ fingerprint, narSize, narHash, references }`.

### Adding the public key to clients

```nix
# nix.conf
trusted-public-keys = cache.nixos.org-1:6NCH... cache.example.com-1:PUBLIC_KEY=
```

Or in NixOS:
```nix
nix.settings.trusted-public-keys = [
  "cache.nixos.org-1:6NCH..."
  "cache.example.com-1:PUBLIC_KEY="
];
nix.settings.substituters = [ "https://cache.example.com" ];
```

### Signing already-built paths

```bash
# Sign all paths in a closure:
nix store sign --key-file /etc/nix/secret.pem \
  --recursive /nix/store/...-my-package

# Sign everything in the store:
nix store sign --key-file /etc/nix/secret.pem --all
```

`nix store sign` writes signatures into the store's SQLite database (`/nix/var/nix/db/db.sqlite`). When `nix serve` or harmonia serves a `.narinfo`, it reads signatures from there.

### Key rotation

1. Generate a new key pair with an incremented suffix: `cache.example.com-2`
2. Add the new public key to clients' `trusted-public-keys` (keep the old key too)
3. Re-sign all existing paths with the new key: `nix store sign --key-file new.pem --all`
4. Once all clients have updated config, remove the old public key from `trusted-public-keys`

The old key remains harmless in `.narinfo` `Sig:` lines — Nix only requires at least one signature from a trusted key, not that all signatures are trusted.

---

## Part 3: Binary Cache Server Implementations

### `nix-serve` — The Original

**Source**: `github.com/edolstra/nix-serve` (Perl, using Starman web server)

The original binary cache server. Serves `.narinfo` and `.nar` files by reading the local Nix store on every request. Single-process, no connection pooling.

**Invocation** (flake-based, current form):
```bash
# Without signing:
nix run github:edolstra/nix-serve -- -p 5000 --access-log /dev/stderr

# With signing (via env var in current version):
NIX_SECRET_KEY_FILE=/etc/nix/secret.pem nix run github:edolstra/nix-serve
```

The original CLI flag form was `nix-serve -p 5000 --sign-key /etc/nix/secret.pem`. The current README instructs setting `NIX_SECRET_KEY_FILE` instead. Signing is **not enabled by default** — without the env var, narinfo files have no `Sig:` field.

**NixOS module** (`services.nix-serve`):

```nix
services.nix-serve = {
  enable = true;
  port = 5000;                               # default: 5000
  bindAddress = "0.0.0.0";                   # default: 0.0.0.0
  secretKeyFile = "/var/cache-priv-key.pem"; # path to signing key
  openFirewall = false;                      # whether to open port in firewall
  extraParams = "--workers 4";               # passed to Starman
  package = pkgs.nix-serve;                  # override to nix-serve-ng
};
```

`secretKeyFile` is the path to the **private** key (the `.pem` file from `nix-store --generate-binary-cache-key`).

**Limitations**:
- Single-process Perl; request concurrency limited to `--workers` forks
- No keepalive or HTTP/1.1 pipelining
- Reads `.nar` by shelling out to `nix-store --dump` on every NAR request — high per-request overhead
- Does not support HTTPS directly; requires nginx/Caddy as reverse proxy
- Does not cache generated NAR or narinfo responses in memory

---

### `nix-serve-ng` — Haskell Rewrite

**Source**: `github.com/aristanetworks/nix-serve-ng` (Haskell, using Warp async HTTP)

A drop-in replacement for `nix-serve` that addresses reliability and throughput. Uses libnixstore directly via Haskell FFI instead of shelling out to `nix-store`.

**Performance** (from upstream benchmarks on a 24-core Xeon + 4 TB SSD):
- NAR info requests: ~100 µs per request
- Empty file NAR: **31.8× faster** than nix-serve (5.16 ms vs 164 ms)
- 10 MB file NAR: **3.35× faster** (86.9 ms vs 291 ms)
- Also outperforms harmonia (Rust) in direct NAR-serving benchmarks

**Drop-in usage** — the recommended approach is to override the package in the `services.nix-serve` NixOS module:

```nix
services.nix-serve = {
  enable = true;
  secretKeyFile = "/var/cache-priv-key.pem";
  package = pkgs.nix-serve-ng;  # drop-in replacement
};
```

No new options are needed. The NixOS module is identical to nix-serve's.

**Unsupported CLI flags** (parsed but ignored): `--workers`, `--preload-app`, `--disable-proctitle`. These reflect Starman-specific flags with no meaning in Warp's async model.

Potentially useful but unimplemented: `--max-requests`, `--user`, `--group`, `--pid`, `--error-log`. The last is relevant for production deployments wanting separate error log files.

**Compatibility notes**: Works with both Nix and Lix package managers. Uses Warp's green-thread model (Haskell async I/O) for concurrent request handling.

---

### `harmonia` — Rust Rewrite

**Source**: `github.com/nix-community/harmonia` (Rust, async, libnixstore FFI)

A Rust rewrite with async request handling, HTTP range request support, transparent zstd compression, and a Prometheus metrics endpoint.

**NixOS module** (via nixpkgs, available since ~2024):

```nix
services.harmonia = {
  enable = true;
  signKeyPaths = [ "/var/lib/secrets/harmonia.secret" ];
  settings = {
    bind = "[::]:5000";
    workers = 4;
    max_connection_rate = 256;
    priority = 30;    # lower = higher priority vs cache.nixos.org (40)
    enable_compression = true;  # zstd; disable on flaky connections
  };
};
```

Or via the flake directly:
```nix
inputs.harmonia.url = "github:nix-community/harmonia";
imports = [ inputs.harmonia.nixosModules.harmonia ];
```

**Client configuration**:
```nix
nix.settings.substituters = [ "https://cache.yourdomain.tld" ];
nix.settings.trusted-public-keys = [ "cache.yourdomain.tld-1:KEY=" ];
```

**Distinctive features**:
- **HTTP range requests**: clients can request byte ranges of NAR files, enabling streaming and partial downloads. This enables `nix store ls` and `nix store cat` to work against harmonia caches without downloading full NARs.
- **Prometheus metrics** at `/metrics`: request counts, latency histograms, cache hit rates. Relevant for integration with the forge-metal ClickStack observability stack.
- **Compression caveat**: zstd compression (when `enable_compression = true`) breaks HTTP range request resumption. Disable `enable_compression` on unreliable network connections or when clients frequently resume interrupted downloads.
- **Built-in TLS**: `tls_cert_path` / `tls_key_path` eliminate the need for a reverse proxy for TLS termination, unlike nix-serve which requires nginx.
- **`/serve/<narhash>/` endpoints**: direct content-addressed serving by NAR hash.

**When to use harmonia vs nix-serve-ng**: harmonia's advantages are the observability endpoint and zstd compression. nix-serve-ng benchmarks faster for raw NAR throughput. For forge-metal's single-operator setup, either works; harmonia's Prometheus integration is a practical advantage.

---

### `attic` — Multi-Tenant Binary Cache

**Source**: `github.com/zhaofengli/attic` (Rust, Apache 2.0, early prototype status)

Attic takes a fundamentally different architectural approach: instead of serving the local Nix store, it is a dedicated server with its own storage layer, multi-tenancy, and global deduplication across tenants.

#### Architecture

**Chunked NAR storage with FastCDC**: NARs are content-split into variable-size chunks using the FastCDC algorithm. Chunks are stored with content-addressed keys. When two different caches push NARs that share common chunks (e.g., glibc appears in many derivations), those chunks are stored only once globally.

Two-level deduplication:
1. **NAR-level**: if an identical NAR already exists in the global NAR store, the new push maps to it directly without chunking.
2. **Chunk-level**: otherwise, the NAR is split; chunks already present are not re-uploaded.

This is the primary architectural distinction from nix-serve/harmonia: those tools are thin HTTP wrappers over the local Nix store. Attic has its own content-addressed storage and deduplication layer.

**Multi-tenancy model**: individual "caches" are restricted views of the shared global NAR and chunk stores. Users pushing to `alice-team` and `bob-team` share deduplicated storage but have isolated access controls.

**Server-side signing**: attic signs NARs itself. Clients do not need a signing key. Push tokens only grant upload access; the private key stays on the server.

#### Server Setup

Attic server (`atticd`) requires:
- **Database**: SQLite (development) or PostgreSQL (production)
- **Storage**: local filesystem or S3-compatible (MinIO, AWS S3, Cloudflare R2)

```bash
# Quick start (SQLite + local storage):
nix shell github:zhaofengli/attic
atticd --config server.toml
```

Minimal `server.toml`:
```toml
[database]
url = "postgresql://attic:password@localhost/attic"

[storage]
type = "local"
path = "/var/lib/attic/storage"

[chunking]
nar-size-threshold = 65536   # NARs smaller than this are not chunked
min-size = 16384
avg-size = 65536
max-size = 262144

[compression]
type = "zstd"

[garbage-collection]
interval = "12 hours"
default-retention-period = "6 months"
```

Production deployment separates components:
```bash
atticd --mode api-server       # stateless, horizontally scalable
atticd --mode garbage-collector # single instance; not replicable
```

#### Client Workflow

```bash
# 1. Login (stores token in ~/.config/attic/config.toml):
attic login myserver https://cache.example.com TOKEN

# 2. Create a cache:
attic cache create myproject

# 3. Make it publicly readable:
attic cache configure myproject --public

# 4. Push a store path (and its closure):
attic push myproject $(nix build .#server-profile --print-out-paths)

# 5. On other machines — configure Nix substituters:
attic use myproject
# ↑ automatically writes to ~/.config/nix/nix.conf:
# substituters = https://cache.example.com/myproject
# trusted-public-keys = cache.example.com:KEY=
```

**Token creation** (on the server):
```bash
atticadm make-token \
  --sub alice \
  --validity "3 months" \
  --pull "alice-*" \
  --push "alice-*" \
  --create-cache "alice-*"
```

**Garbage collection**: operates at three levels — local cache mappings, global NAR store, global chunk store. Run manually or configured via `garbage-collection.interval`.

#### Limitations and Status

- **Early prototype**: labeled as such in upstream README; production stability not guaranteed as of early 2026.
- **Storage complexity**: requires a PostgreSQL server and S3-compatible storage for production use. More operational overhead than nix-serve/harmonia.
- **Push model**: clients push explicitly (`attic push cache path`). There is no equivalent to `nix-serve`'s passive serving of the local store. Paths must be pushed before they are available as substitutes.
- **No S3-only mode**: unlike the pattern of `nix copy --to s3://bucket`, attic requires a running server process to mediate access.
- **Attic vs narra/other forks**: the upstream `zhaofengli/attic` is the reference; there are community forks but none are widely adopted.

---

### `cachix` — Hosted Service

**Source**: `cachix.org` (SaaS), `github.com/cachix/cachix` (CLI, Haskell)

Cachix is the dominant hosted binary cache service in the Nix ecosystem. The CLI and GitHub Actions integration are the primary touch points.

**Basic workflow**:
```bash
# 1. Create a cache at cachix.org, get an auth token
cachix authtoken TOKEN

# 2. On a build machine — push all outputs of a build:
nix build .#server-profile
cachix push my-cache-name $(nix path-info --recursive .#server-profile)

# 3. Configure Nix to use it (writes to nix.conf):
cachix use my-cache-name
```

**GitHub Actions integration** (`cachix/cachix-action`):
```yaml
- uses: cachix/cachix-action@v15
  with:
    name: my-cache-name
    authToken: ${{ secrets.CACHIX_AUTH_TOKEN }}
    # Automatically pushes all built paths after each step
```

The action installs Nix (via `install-nix-action`) and sets up the cache as a substituter. All derivations built in the workflow are pushed automatically.

**Free tier limits** (as of early 2026): 5 GB storage, 1 user. Paid plans scale to team usage and larger caches.

**Self-hosted cachix**: Cachix does not offer a self-hosted version of the server. The CLI is open source but requires the cachix.org backend. For fully self-hosted deployments (forge-metal requirement), use harmonia, nix-serve-ng, or attic instead.

**Relevant for forge-metal**: cachix violates the hard "everything self-hosted" requirement. The `cachix push` CLI pattern is the reference for the post-build-hook pattern below, but the server must be replaced with harmonia or nix-serve-ng.

---

## Part 4: The `post-build-hook` Mechanism

The post-build hook is the standard mechanism for automatically uploading freshly-built derivations to a binary cache.

### Configuration

In `nix.conf`:
```
post-build-hook = /etc/nix/upload-to-cache.sh
```

Or in NixOS:
```nix
nix.settings.post-build-hook = "/etc/nix/upload-to-cache.sh";
```

This setting is **trusted-user only**: it can only be set in the system `nix.conf` or by users listed in `trusted-users`. Unprivileged users cannot inject post-build hooks.

### Environment variables

The hook script receives:
- `$OUT_PATHS`: space-separated list of output store paths that were just built (e.g., `/nix/store/abc...-bash-5.2` or multiple paths for multi-output derivations)
- `$DRV_PATH`: path to the `.drv` file for the build (e.g., `/nix/store/xyz...-bash-5.2.drv`)

### Execution semantics

- Runs **after each successful build** — substituted paths do NOT trigger the hook (only locally-built outputs)
- Runs **synchronously**, blocking the Nix daemon's build loop until the hook exits
- Runs as **root** when using `nix-daemon` (multi-user Nix); as the calling user in single-user Nix
- If the hook **exits non-zero**, the current build loop exits (not just this hook invocation)
- Hook output (stdout/stderr) goes to the user's terminal

### Minimal S3 upload hook

```bash
#!/bin/bash
set -eu
set -f # disable glob expansion on $OUT_PATHS

# Sign and upload to S3:
nix copy --to "s3://my-nix-cache?region=us-east-1&secret-key=/etc/nix/cache.pem" \
  $OUT_PATHS
```

`-f` (no glob expansion) is important: store paths contain hyphens and dots that could match shell globs.

### Signing in the hook vs signing in nix.conf

Two signing approaches:

**Approach A — sign in nix.conf** (recommended for nix-serve/harmonia):
```
secret-key-files = /etc/nix/cache.pem
```
All locally-built paths are signed immediately as they land in the store. The hook just copies already-signed paths.

**Approach B — sign in the hook** (required if the key is not on the build machine):
```bash
nix store sign --key-file /etc/nix/cache.pem $OUT_PATHS
nix copy --to "https://cache.example.com" $OUT_PATHS
```

### Blocking gotcha and background upload pattern

The hook blocking the build loop is a significant practical problem: if the cache is slow or the network is unreliable, every build is stalled.

Recommended pattern for production — write to a queue file and have a separate daemon pick it up:

```bash
#!/bin/bash
# Non-blocking post-build hook:
set -f
printf "%s" "$OUT_PATHS" >> /var/lib/nix-upload-queue/$(date +%s%N)
```

A separate systemd service polls the queue directory and uploads asynchronously. This decouples build latency from upload latency.

---

## Part 5: S3 Binary Cache

### URL format

Nix S3 store URL: `s3://bucket-name?param=value&...`

Full parameter reference:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `region` | string | `us-east-1` | AWS region |
| `endpoint` | string | (empty) | Override endpoint URL for S3-compatible storage |
| `scheme` | string | (empty) | `https` or `http` for requests |
| `profile` | string | (empty) | AWS credential profile from `~/.aws/credentials` |
| `compression` | enum | `xz` | NAR compression: `xz`, `bzip2`, `gzip`, `zstd`, `none` |
| `compression-level` | int | `-1` | Compression preset; -1 = library default |
| `parallel-compression` | bool | `false` | Multi-threaded compression (xz/zstd only) |
| `multipart-upload` | bool | `false` | Use S3 multipart upload for large NARs |
| `secret-key` | string | (empty) | Path to Ed25519 signing key (signs .narinfo at upload time) |
| `priority` | int | 0 | Substituter priority |
| `want-mass-query` | bool | `false` | Allow batch narinfo queries |

### Uploading to S3

```bash
# Upload with AWS profile auth + signing:
nix copy \
  --to 's3://my-nix-cache?region=us-east-1&profile=nix-upload&secret-key=/etc/nix/cache.pem' \
  /nix/store/...-my-package

# Upload entire build closure recursively:
nix copy \
  --to 's3://my-nix-cache?region=us-east-1&profile=nix-upload' \
  --recursive \
  /nix/store/...-server-profile
```

### S3-compatible endpoints (MinIO, Cloudflare R2, Backblaze B2)

```bash
# Cloudflare R2 (no egress fees — popular for large caches):
nix copy \
  --to 's3://my-nix-cache?endpoint=https://ACCOUNT_ID.r2.cloudflarestorage.com&region=auto' \
  /nix/store/...-my-package

# Backblaze B2:
nix copy \
  --to 's3://my-bucket?endpoint=https://s3.us-west-002.backblazeb2.com&region=us-west-002' \
  /nix/store/...-my-package

# MinIO (local):
nix copy \
  --to 's3://my-nix-cache?endpoint=https://minio.internal:9000&region=us-east-1&scheme=https' \
  /nix/store/...-my-package
```

R2 is increasingly popular as a Nix binary cache backend: no egress fees for reads, S3-compatible API, and $0.015/GB/month storage. A Cloudflare Worker or `attic` server can front R2 for narinfo serving.

### Client configuration

```nix
# NixOS nix.settings:
nix.settings = {
  substituters = [
    "https://cache.nixos.org"
    "https://my-nix-cache.s3.amazonaws.com"
    # or via R2 custom domain:
    "https://nix-cache.example.com"
  ];
  trusted-public-keys = [
    "cache.nixos.org-1:6NCH..."
    "my-nix-cache:PUBLIC_KEY="
  ];
};
```

Or in `flake.nix` (prompts the user):
```nix
nixConfig = {
  extra-substituters = [ "https://nix-cache.example.com" ];
  extra-trusted-public-keys = [ "nix-cache.example.com-1:KEY=" ];
};
```

### Local binary cache (`file://`)

For air-gapped systems or offline CI:

```bash
# Create a local cache from a closure:
nix copy --to file:///tmp/nix-cache /nix/store/...-my-package

# Restore from local cache:
nix copy --from file:///tmp/nix-cache /nix/store/...-my-package

# Mount in nix.conf:
# substituters = file:///mnt/shared/nix-cache
```

The `file://` store creates the standard narinfo + nar directory structure on disk, identical to what `nix-serve` would serve over HTTP.

---

## Part 6: Binary Cache Comparison

| Tool | Language | Backend | Signing | Multi-tenancy | Overhead |
|------|----------|---------|---------|---------------|----------|
| `nix-serve` | Perl | local Nix store | optional (`NIX_SECRET_KEY_FILE`) | no | high (shell out per request) |
| `nix-serve-ng` | Haskell | libnixstore FFI | same as nix-serve | no | low (31× faster) |
| `harmonia` | Rust | libnixstore FFI | via `signKeyPaths` | no | low; async |
| `attic` | Rust | own S3/PostgreSQL | server-side | yes | high (separate DB + storage) |
| `cachix` | SaaS | cachix.org | managed | yes | none (hosted) |
| `nix copy --to s3://` | — | S3 | via `secret-key=` param | no | per-upload only |

For forge-metal (single-operator, self-hosted): **harmonia** is the best fit. It's async, has Prometheus metrics for ClickStack integration, supports zstd, has a stable NixOS module in nixpkgs, and is simpler than attic without the PostgreSQL/S3 dependency.

---

## Part 7: Flake Input Fetcher Types — Complete Reference

Flake inputs are declared in `flake.nix` with a URL string or a structured attribute set. Every input is locked in `flake.lock` with a content-addressed NAR hash.

### Structured vs URL-string form

Both forms are equivalent. The structured form is needed when setting non-URL parameters like `flake` or `submodules`:

```nix
# URL string form:
inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

# Structured attribute form:
inputs.my-repo = {
  type = "git";
  url = "https://github.com/user/repo.git";
  ref = "main";
  submodules = true;
  flake = false;
};

# Extra-input attributes (work in both forms):
inputs.nixpkgs.follows = "another-input/nixpkgs";
inputs.nixpkgs.flake = false;
```

### `github:<owner>/<repo>[/<rev-or-ref>]`

- **Fetcher**: downloads a tarball from GitHub's archive API — NOT a git clone
  - Authenticated: `https://api.github.com/repos/{owner}/{repo}/tarball/{rev}`
  - Public: `https://github.com/{owner}/{repo}/archive/{rev}.tar.gz`
- **Lock format**: `{ type="github"; owner; repo; rev; narHash; lastModified }`
- **No `revCount`**: because the archive API does not return commit history, git rev-list count is unavailable
- **Parameters**:
  - `ref=<branch-or-tag>`: resolve HEAD of branch (not locked until `nix flake update`)
  - `rev=<commit-sha>`: pin to specific commit
  - `host=<hostname>`: for GitHub Enterprise (`github:myorg/myrepo?host=github.mycompany.com`)
- **Submodules**: the GitHub archive tarball does not include submodule content. For repos with submodules use `git+https:` instead with `submodules=1`
- **Tarball cache**: fetched tarballs are cached in `/nix/var/nix/tarball-ttl`; `--refresh` forces re-fetch

```nix
inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
inputs.my-fork.url = "github:myorg/nixpkgs/feature-branch?host=github.enterprise.com";
```

### `gitlab:<owner>/<repo>[/<rev-or-ref>]`

- **Fetcher**: tarball from GitLab archive API (analogous to github:)
- **Parameters**: same as `github:` plus `host=<hostname>` for self-hosted instances
- **Nested subgroups**: slashes in nested group paths must be percent-encoded as `%2F`
  ```nix
  inputs.veloren-rfcs.url = "gitlab:veloren%2Fdev/rfcs";
  ```
- **Lock format**: `{ type="gitlab"; owner; repo; rev; narHash; lastModified }`

```nix
inputs.my-pkg.url = "gitlab:mygroup/myrepo?host=gitlab.mycompany.com";
```

### `sourcehut:<owner>/<repo>[/<rev-or-ref>]`

- **Fetcher**: tarball from sr.ht API
- **Parameters**: `ref` (Git only), `rev`, `host` (for `git.sr.ht` or `hg.sr.ht`)
- **Mercurial limitation**: `ref` name resolution only works for Git repositories. For Mercurial repos on sr.ht, `rev` must be specified explicitly.

```nix
inputs.my-hg-repo = {
  url = "sourcehut:~user/myrepo";
  rev = "abc123def456";  # required for Mercurial
};
```

### `git+https:`, `git+ssh:`, `git+http:`

Full git clone. Slower than the archive-API fetchers (`github:`, `gitlab:`) but required when:
- The repo host does not support the archive API
- Submodules are needed
- `revCount` is needed (git history is present)
- The host is not GitHub/GitLab/sr.ht

**URL format**: `git+https://host/path?param=value`

**Parameters**:

| Parameter | Type | Description |
|-----------|------|-------------|
| `ref` | string | Branch or tag name (default: `HEAD`) |
| `rev` | string | Exact commit SHA |
| `dir` | string | Subdirectory containing `flake.nix` |
| `submodules` | bool | Fetch git submodules (`submodules=1`) |
| `shallow` | bool | Shallow clone — only fetch tip commit (`shallow=1`) |
| `allRefs` | bool | Fetch all refs (needed for some tag patterns) |

**Lock format**: `{ type="git"; url; rev; lastModified; narHash; revCount }`

```nix
# Standard git+https:
inputs.my-repo.url = "git+https://codeberg.org/user/repo?ref=main";

# With submodules:
inputs.my-repo = {
  url = "git+https://github.com/user/repo?submodules=1";
};

# Shallow clone (critical for large repos like linux kernel):
inputs.linux-src = {
  url = "git+https://github.com/torvalds/linux?shallow=1&ref=v6.7";
  flake = false;
};
```

**`shallow = true` performance**: avoids fetching the full git history. For repositories with thousands of commits, the difference can be 100 MB vs 2 GB. Use `shallow=1` whenever `revCount` is not needed.

**`allRefs = true`**: by default, git fetchers only fetch the specified `ref`. Setting `allRefs=1` fetches all refs, which is required when the `ref` is a tag that is not reachable from the default branch. In practice, this flag is rarely needed.

**Submodules in Nix 2.27+**: when a flake declares `inputs.self.submodules = true`, callers of that flake no longer need to pass `submodules = true` themselves. The flake self-declares its submodule requirement.

### `git+file:<path>` and local git repos

`git+file:` points to a local git repository. Unlike `path:`, it uses git object semantics rather than filesystem mtime:

```nix
inputs.local-repo.url = "git+file:///home/user/my-project";
```

When no `ref` or `rev` is given, files are fetched directly from the local filesystem path (not from git objects). This makes `git+file:` behave like `path:` for unspecified refs.

**Key distinction from `path:`**: a `path:` input's lock is based on the narHash of the directory content. A `git+file:` input's lock includes a `rev` (git commit SHA) when a ref is specified, making it deterministic relative to git history.

```nix
# Force locking to a git commit (not just filesystem state):
inputs.local-lib.url = "git+file:///home/user/nix-lib?ref=main";
```

### `path:<path>` and `./relative/path`

Points to a local directory. Relative paths must start with `./` to distinguish them from registry lookups:

```nix
inputs.local-modules.url = "path:./modules";   # relative to flake.nix
inputs.abs-path.url = "path:/opt/my-nix-lib";  # absolute path
```

**`flake = false` with path inputs**: useful for including local non-flake directories as build inputs:

```nix
inputs.my-scripts = {
  url = "path:./scripts";
  flake = false;
};
# In outputs: src = inputs.my-scripts;  (the outPath is the directory)
```

**Lock format**: `{ type="path"; path; narHash; lastModified }`

**Within-tree constraint**: path inputs must stay within the flake's source tree or use absolute paths. `path:../outside` requires the parent directory to be accessible and tracked.

**`narHash` in locks**: the narHash of path inputs changes whenever any tracked file in the directory changes, causing downstream cache misses. This is the motivation for `lib.fileset.gitTracked` (see `deployment-and-secrets.md`).

### `tarball:` and `file:`

For fetching non-VCS sources:

```nix
# Tarball (any HTTP archive):
inputs.my-release.url = "tarball+https://example.com/release-v1.0.tar.gz";
# Short form (when extension is recognized):
inputs.my-release.url = "https://example.com/release-v1.0.tar.gz";

# Single file:
inputs.config-file.url = "file+https://example.com/config.json";
```

**Supported archive formats for tarball:**: `.zip`, `.tar`, `.tgz`, `.tar.gz`, `.tar.xz`, `.tar.bz2`, `.tar.zst`

**Lock format**: `{ type="tarball"; url; narHash; lastModified }` — no `rev` field (locked by content hash only)

**Why tarball has no `rev`**: the tarball URL may not be a VCS, so there is no commit hash. The narHash provides content integrity instead. If the remote URL serves different content at the same URL over time (no content-addressed hosting), the lock will not detect the change until `nix flake update` is run.

**`tarball:` vs `github:`**: both download tarballs, but `github:` parses the GitHub URL format and adds GitHub-specific metadata (owner, repo, rev from the API). `tarball:` is a generic fetcher — it knows nothing about the source's version history.

### `hg+https:`, `hg+ssh:`

Mercurial repositories. Mirror the git fetcher scheme:

```nix
inputs.my-hg-repo.url = "hg+https://bitbucket.org/user/repo";
```

Rarely used in practice. The Nix ecosystem has largely moved to git.

### The `dir=` query parameter

Every fetcher type supports `dir=<subdir>` to locate `flake.nix` in a subdirectory:

```nix
# Monorepo with flake.nix in a subdirectory:
inputs.my-pkg.url = "github:myorg/monorepo?dir=packages/my-pkg";

# git clone with subdirectory:
inputs.my-lib.url = "git+https://example.com/repo?ref=main&dir=nix";
```

The `dir` parameter means Nix fetches the entire repository but only evaluates `flake.nix` from the specified subdirectory.

### The `flake = false` attribute

When set on an input, Nix fetches the source but does not evaluate it as a flake:

```nix
inputs.some-tool-src = {
  url = "github:some/tool";
  flake = false;
};
```

In `outputs`, the input is just an attrset with an `outPath` attribute pointing to the fetched directory:
```nix
outputs = { self, some-tool-src, ... }:
  stdenv.mkDerivation {
    src = some-tool-src;  # coerces to some-tool-src.outPath
  };
```

**Use cases**:
- Including non-Nix sources (configuration files, data, scripts) as build inputs
- Using a repo as source in a derivation without pulling in its Nix evaluation overhead
- Vendoring a library that ships a `flake.nix` you don't want to evaluate

**Lock behavior**: `flake = false` inputs are still locked with `narHash` (and `rev` if VCS). They participate in `nix flake update`.

### `narHash` attribute in locked inputs

Every locked input has a `narHash` field in `flake.lock`. This is the SHA-256 of the NAR serialization of the fetched source tree, in SRI format (`sha256-<base64>`).

When re-fetching a locked input, Nix verifies the downloaded content produces the same narHash. This makes flake inputs tamper-evident: if the remote host changes the content at a URL, `nix build` detects the mismatch and fails.

### `follows` — Deduplicating Shared Inputs

`inputs.X.follows = "Y"` makes input `X` use the same locked revision as `Y`. This avoids multiple versions of nixpkgs in the dependency graph:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";  # use OUR nixpkgs
    };
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
  };
}
```

Without `follows`, `home-manager` and `flake-parts` would each have their own nixpkgs instance, resulting in three nixpkgs store paths and binary cache misses for all three.

`follows` accepts slash-separated paths for transitive inputs: `"dwarffs/nixpkgs"` means "use the nixpkgs that dwarffs uses."

---

## Part 8: Fetcher Type Quick-Reference

| URL scheme | Fetcher | Supports `rev` | Supports `ref` | Supports submodules | Has `revCount` | Lock field |
|-----------|---------|----------------|----------------|--------------------|----|-----------|
| `github:` | archive tarball | yes | yes | no (use `git+https:`) | no | `owner/repo/rev` |
| `gitlab:` | archive tarball | yes | yes | no | no | `owner/repo/rev` |
| `sourcehut:` | archive tarball | yes | git only | no | no | `owner/repo/rev` |
| `git+https:` | full git clone | yes | yes | `submodules=1` | yes | `url/rev` |
| `git+ssh:` | full git clone | yes | yes | `submodules=1` | yes | `url/rev` |
| `git+file:` | local git | yes (if ref set) | yes | `submodules=1` | yes | `url/rev` |
| `path:` | filesystem | no | no | n/a | no | `narHash` only |
| `tarball:` | HTTP tarball | no | no | no | no | `narHash` only |
| `file:` | HTTP file | no | no | no | no | `narHash` only |
| `hg+https:` | Mercurial | yes | yes (hg only) | no | yes | `url/rev` |
| `indirect:` | registry lookup | resolves to above | resolves to above | — | — | resolved type |

---

## Part 9: Relevance to forge-metal

### Which binary cache to deploy

For a single-operator bare-metal CI platform:

1. **Immediate need**: `nix-serve-ng` with NixOS module. Drop-in replacement, no extra dependencies, highest raw throughput for serving the server-profile closure to developer machines.

2. **Better fit for observability integration**: `harmonia`. The Prometheus `/metrics` endpoint integrates with the ClickStack via the OTel Collector. Set `services.harmonia.signKeyPaths` to automatically sign all served NARs.

3. **Skip attic** unless multi-tenancy or global NAR deduplication across many nodes is needed. The PostgreSQL + S3 dependency adds operational complexity that isn't justified for a single-node setup.

### S3 binary cache for the server-profile

The `server-profile` closure is ~2 GB. Pushing it to Cloudflare R2 after each CI build means developer machines and remote nodes can substitute it without re-building:

```bash
# In a post-build-hook or after make server-profile:
nix copy \
  --to 's3://forge-metal-cache?endpoint=https://ACCOUNT_ID.r2.cloudflarestorage.com&region=auto&secret-key=/etc/nix/cache.pem' \
  $(nix build .#server-profile --print-out-paths)
```

In `flake.nix`:
```nix
nixConfig = {
  extra-substituters = [ "https://forge-metal-cache.ACCOUNT_ID.r2.cloudflarestorage.com" ];
  extra-trusted-public-keys = [ "forge-metal-cache-1:PUBLIC_KEY=" ];
};
```

R2 has no egress fees, making it cost-effective for a cache that is read frequently by CI nodes.

### Flake input type for internal libraries

If forge-metal grows internal Nix libraries in subdirectories:
- Use `path:./lib/my-module` (or just `./lib/my-module`) for same-repo references — no network, locked by narHash
- Use `git+https:?shallow=1` for pinning external repos that don't publish to GitHub/GitLab archive API
- Always set `inputs.X.inputs.nixpkgs.follows = "nixpkgs"` for any new flake input that itself depends on nixpkgs, to avoid binary cache misses from multiple nixpkgs instances
