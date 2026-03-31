# Sealed Registry Pattern

The core idea: warm the cache while connected to npmjs.org, then remove the proxy
directives so Verdaccio serves entirely from disk. No outbound network during CI.

## Warmup Phase (Online)

Config during warmup:

```yaml
uplinks:
  npmjs:
    url: https://registry.npmjs.org/
    cache: true        # Store tarballs to disk
    maxage: 30m        # Don't re-check metadata every 2 minutes
    timeout: 60s       # Patient on first fetch
    max_fails: 3
    fail_timeout: 10m

packages:
  '**':
    access: $all
    publish: $authenticated
    proxy: npmjs
```

Run `npm install` for every project that will run in CI. Verdaccio downloads and caches
all tarballs + metadata to `/var/lib/verdaccio/storage/`.

### What gets cached

- **Tarballs**: every `.tgz` file for every version resolved by the lockfile
- **Metadata**: `package.json` manifest for every package in the dependency tree
- **Not cached**: `npm audit` data, search index, user/org metadata

### Warmup strategy for forge-metal

For each fixture repo (e.g. `next-bun-monorepo`, `next-pnpm-postgres`):

1. Boot a Firecracker VM with the unsealed Verdaccio as registry
2. Run `npm install` / `pnpm install` / `bun install`
3. This populates the Verdaccio cache with exactly the packages needed
4. Repeat for all fixture repos
5. Seal the registry

This is better than blindly caching the npm top-1000 because it caches exactly what's
needed and nothing more.

## Seal Phase (Offline)

Remove all `proxy` directives from package patterns. The existing `seal-registry.sh`
does this correctly:

```bash
# seal: strips proxy keys from all package patterns via Python/YAML
# unseal: adds proxy: npmjs back to all package patterns
# status: checks if proxy appears in config
```

After sealing:
- `npm install` resolves entirely from Verdaccio's local storage
- No DNS lookups, no TLS handshakes, no npmjs.org dependency
- Verdaccio becomes a simple static file server with npm protocol

## What Breaks Offline

| Feature | Behavior When Sealed | Mitigation |
|---------|---------------------|------------|
| `npm audit` | **Fails.** Audit middleware proxies to npmjs. | Use `--no-audit` in CI |
| `npm search` | Returns only locally published packages, not cached upstream | Not needed in CI |
| New dependencies | **Fails.** Any package not in cache returns 404 | Unseal, install, inspect, reseal |
| `npm login` / `npm adduser` | Works (local htpasswd) | N/A |
| `npm publish` | Works for private packages | N/A |
| Lockfile integrity check | Works — tarballs have correct sha512 | N/A |
| `npx` | Fails if package not cached | Pre-cache any npx tools during warmup |

### The npm audit problem in detail

`npm audit` sends a POST to `/-/npm/v1/security/audits`. Verdaccio's audit middleware
(formerly `verdaccio-audit`, now `plugins/audit/` in the monorepo) does **not** perform
local vulnerability analysis. It proxies the request to `https://registry.npmjs.org`.

This means:
1. Even with a sealed registry, `npm audit` makes an outbound request
2. In a truly air-gapped network, `npm audit` returns an error
3. The middleware is marked "experimental and unstable" in the docs

**For CI, always use `--no-audit`:**

```bash
npm install --no-audit --prefer-offline
# or
pnpm install --no-optional  # pnpm doesn't audit on install by default
# or
bun install  # bun doesn't run npm audit
```

## Incremental Updates

When a project adds a new dependency:

1. `unseal` the registry (restores `proxy: npmjs`)
2. Run `npm install` for the project (caches new packages)
3. Run `inspect-tarballs.sh` (check for lifecycle scripts, binding.gyp, etc.)
4. `seal` the registry

The existing `scripts/security/` tooling handles this correctly. The inspect step is
important because supply-chain attacks typically arrive via lifecycle scripts
(`preinstall`, `postinstall`) in new dependencies.

## Pre-Population Alternatives

| Method | How | When to use |
|--------|-----|-------------|
| Install through Verdaccio | `npm install` with proxy active | Normal warmup (our approach) |
| Copy storage directory | `rsync` from another Verdaccio instance | Cloning a known-good mirror |
| `npm pack` + `npm publish` | Pack each package, publish to Verdaccio | When you need to vendor specific versions |
| Populate from lockfile | Parse lockfile, download tarballs directly to storage | Automation without running install |

The first method (install through Verdaccio) is simplest and what forge-metal already
uses. It has the advantage of exercising the exact resolution path that CI will use.

## Storage Size Estimates

Measured from real Next.js project dependency trees:

| Project Type | Approx. Packages | Tarball Cache Size |
|--------------|-------------------|-------------------|
| Simple Next.js app | ~300 | ~200 MB |
| Next.js + Tailwind + Prisma | ~500 | ~400 MB |
| Turborepo monorepo (2 apps) | ~600 | ~500 MB |
| Large monorepo (5+ apps, shared libs) | ~1000+ | 1–3 GB |

Metadata overhead is negligible (<50 MB for 1000 packages). Total storage for forge-metal
with 2 fixture repos: expect **1–2 GB**.
