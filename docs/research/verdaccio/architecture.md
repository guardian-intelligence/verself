# Verdaccio Architecture

## Source Layout

Verdaccio is a monorepo. The packages that matter for understanding caching and storage:

| Package | Purpose |
|---------|---------|
| `packages/store/src/storage.ts` | Central orchestrator — `getPackageByOptions()`, `getTarball()`, `updateManifest()` |
| `packages/config/src/conf/default.yaml` | Authoritative default config shipped with every install |
| `packages/core/core/src/plugin-utils.ts` | `IPluginStorage<T>` and `IPackageStorage` interfaces |
| `@verdaccio/local-storage` | Default filesystem backend, uses `lowdb` for `.verdaccio-db.json` |
| `@verdaccio/file-locking` | Filesystem-level locking for concurrent access |
| `plugins/audit/` | npm audit middleware (proxies to npmjs, does not analyze locally) |

Current stable: **6.3.2**. Version 9.x (master) targets Node.js 24+ and is experimental.
The forge-metal pin is 6.1.2 — should bump to 6.3.2.

## On-Disk Layout (local-storage)

```
/var/lib/verdaccio/storage/
├── .verdaccio-db.json           # Package index (lowdb). Single point of state.
├── lodash/
│   ├── package.json             # Metadata manifest (npm registry format)
│   └── lodash-4.17.21.tgz      # Cached tarball (only if cache: true)
└── @next/
    └── env/
        ├── package.json
        └── @next/env-14.2.3.tgz
```

`.verdaccio-db.json` is the scaling bottleneck. It stores:
- List of all private (locally published) packages
- A server secret used for token generation
- No metadata about cached upstream packages — those are tracked by the presence of
  `<pkg>/package.json` files on disk

## Tarball Caching vs Metadata Caching

This distinction is critical for disk planning and for understanding what "sealed" means.

### Metadata (always cached)

- Package manifests (`package.json` per package) are **always** written to disk, regardless
  of the `cache` uplink setting.
- Controlled by `maxage` (default: **2 minutes**). After expiry, Verdaccio re-fetches
  metadata from the uplink on next request.
- Typically 1–100 KB per package. Packages with many versions (`lodash`, `typescript`) have
  larger manifests but still small relative to tarballs.

### Tarballs (controlled by `cache: true/false`)

- When `cache: true` (default): tarballs are downloaded once and stored to disk.
- When `cache: false`: tarballs stream through on every request. Only metadata folders exist
  on disk.
- **This is the disk usage driver.** A typical Next.js project: ~700 MB–1 GB of tarballs
  for the full dependency tree.

### Tarball Resolution Flow

From `packages/store/src/storage.ts`:

```
1. getTarball(name, filename) called by HTTP handler
2. getLocalTarball() checks local storage
3. Cache hit  → stream from disk immediately (no uplink contact)
4. Cache miss → getTarballFromUpstream() fetches from uplink
5. Fetched tarball is asynchronously written to local storage
6. Subsequent requests served from disk
```

Step 5 is non-blocking: the tarball is streamed to the client and written to disk
concurrently. If the write fails (disk full, permissions), the client still gets the
tarball but it won't be cached.

## Uplinks (Proxy Behavior)

Full property reference:

| Property | Default | Description |
|----------|---------|-------------|
| `url` | required | Upstream registry URL |
| `timeout` | `30s` | Per-request timeout |
| `maxage` | **`2m`** | Metadata cache TTL. After expiry, re-fetches from upstream |
| `fail_timeout` | `5m` | Cooldown after uplink marked down |
| `max_fails` | `2` | Consecutive failures before marking uplink down |
| `cache` | **`true`** | Store tarballs locally. `false` = metadata-only |
| `strict_ssl` | `true` | Validate upstream TLS certificates |
| `agent_options` | none | HTTP agent settings (`maxSockets`, etc.) |
| `auth` | disabled | Bearer token for authenticated uplinks |

### Retry Logic

Simple fail-count + cooldown. No exponential backoff.

1. Request to uplink fails
2. Increment fail counter
3. After `max_fails` consecutive failures, mark uplink as down
4. After `fail_timeout`, reset and retry on next request

### Request Fan-Out

For each client request, Verdaccio calls **every uplink** matching the package pattern.
If you configure 3 uplinks with `proxy: npmjs,other1,other2`, every metadata request
fans out to all three. For a single-uplink setup (our case), this is irrelevant.

## Storage Plugin Interface

Two interfaces from `packages/core/core/src/plugin-utils.ts`:

```typescript
// Global storage — one per Verdaccio instance
interface IPluginStorage<T> {
  add(name: string): Promise<void>;
  remove(name: string): Promise<void>;
  get(): Promise<StorageList>;
  getSecret(): Promise<string>;
  setSecret(secret: string): Promise<void>;
  getPackageStorage(packageName: string): IPackageStorage;
}

// Per-package storage — one per request
interface IPackageStorage {
  readPackage(name: string): Promise<Package>;
  savePackage(name: string, value: Package): Promise<void>;
  readTarball(name: string): ReadTarball;
  writeTarball(name: string): WriteTarball;
  deletePackage(fileName: string): Promise<void>;
}
```

Available storage plugins:

| Plugin | Backend | Notes |
|--------|---------|-------|
| `@verdaccio/local-storage` | Filesystem | Default. Uses file locking. Production-proven. |
| `verdaccio-aws-s3-storage` | S3-compatible | Supports CDN via `tarballACL: public-read` |
| `verdaccio-google-cloud` | GCS | Requires keyfile |
| `verdaccio-memory` | RAM | Testing only, lost on restart |

For single-node bare metal, local-storage is correct. S3 adds latency and complexity with
no benefit when everything is on the same machine.
