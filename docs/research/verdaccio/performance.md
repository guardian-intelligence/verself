# Verdaccio Performance and Scaling

## Single-Node Performance (forge-metal's deployment)

Verdaccio is a single-process Node.js application. For a sealed registry serving cached
tarballs, it is essentially a static file server with npm protocol handling. Performance
characteristics:

### Memory

- Baseline: ~50–100 MB RSS
- Grows with concurrent connections and manifest size in memory
- No persistent in-memory cache of tarballs (streamed from disk)
- `.verdaccio-db.json` is loaded into memory via lowdb

### Disk I/O

Sealed registry = all reads. The access pattern during CI `npm install`:

1. Client requests package metadata → Verdaccio reads `<pkg>/package.json` from disk
2. Client requests tarball → Verdaccio streams `<pkg>/<pkg>-<ver>.tgz` from disk

For a warm OS page cache (likely after the first CI run), this is nearly zero-cost.
ZFS's ARC (Adaptive Replacement Cache) will keep hot tarballs in RAM.

### Concurrent CI Jobs

forge-metal runs multiple Firecracker VMs hitting the same Verdaccio instance. The
`@verdaccio/file-locking` package provides filesystem-level locking:

- **Reads are concurrent** — file locking is only for write operations
- **Writes only happen during warmup** (unsealed, caching new tarballs)
- **After sealing, contention is zero** — all operations are reads

For 10 concurrent VMs each running `npm install` against a sealed Verdaccio, the
bottleneck will be disk I/O (or more likely, nothing — ZFS ARC handles this).

### Tuning for CI

```yaml
# Increase keepalive for connection reuse across concurrent installs
server:
  keepAliveTimeout: 120

# Increase body size limit for large monorepo manifests
max_body_size: 100mb
```

The systemd unit in forge-metal already sets `CPUAffinity=0 1`, pinning Verdaccio to
2 cores. For a sealed read-only registry, even 1 core is sufficient.

## Scaling Walls

### The .verdaccio-db.json bottleneck

The primary scaling limitation. This file:
- Is read/written by a single Node.js process via lowdb
- Has no built-in replication or sharding
- Is not safe for concurrent writers (no PM2 cluster mode)
- Even the S3 storage plugin still uses this file (confirmed by
  [issue #1459](https://github.com/verdaccio/verdaccio/issues/1459))

**For forge-metal this is irrelevant** — single-node, single Verdaccio process, and the
file is only written during warmup.

### No horizontal scaling

Verdaccio has no built-in clustering. The maintainer (Juan Picado) acknowledged in
issue #1459 that no "crazy experiments" with scaling had been conducted. Multi-instance
deployments require external coordination (shared storage + sticky sessions).

**Again, irrelevant for forge-metal.** If you ever need multi-node, the correct
architecture is one Verdaccio per node, each with its own sealed storage.

### Large registries

No published benchmarks exist for Verdaccio serving 10,000+ packages. For forge-metal's
use case (~300–1000 packages per fixture repo), this is not a concern. The full npmjs
registry is ~3 million packages — Verdaccio is not designed to mirror all of npmjs.

## Comparison with Alternatives

| | Verdaccio | Nexus | Artifactory | cnpmcore | npm-proxy-cache |
|---|-----------|-------|-------------|----------|-----------------|
| **License** | MIT | EPL-1.0 / Commercial | Commercial ($150+/mo) | MIT | MIT |
| **Setup** | `npm i -g verdaccio` | Java/WAR | Docker/installer | Node.js + DB | `npm i -g` |
| **Formats** | npm only | 18+ | 30+ | npm only | npm only |
| **Memory** | ~50–100 MB | ~1–2 GB (Java) | ~1–2 GB (Java) | ~200 MB | ~30 MB |
| **Air-gapped** | Yes (remove proxy) | Yes (hosted mode) | Yes | Yes | No |
| **Clustering** | No | Pro only | Yes | Yes (with DB) | No |
| **Auth** | htpasswd, LDAP | LDAP, SAML, SSO | Full enterprise | Built-in | None |

### When Verdaccio is the right choice

- Single-node deployment (forge-metal: yes)
- npm-only (forge-metal: yes)
- Minimal operational overhead (forge-metal: yes, don't want to run a JVM)
- Sealed/air-gapped operation (forge-metal: yes)
- No budget for commercial tooling (forge-metal: yes, open-source project)

### When Verdaccio is the wrong choice

- Multi-format registry (Docker, Maven, etc.) → Nexus or Artifactory
- Multi-node HA with shared state → cnpmcore (with DB) or Artifactory
- Full npmjs mirror at scale → cnpmcore (powers registry.npmmirror.com)

### cnpmcore

[cnpmcore](https://github.com/cnpm/cnpmcore) powers China's npm mirror
(`registry.npmmirror.com`). It requires MySQL or PostgreSQL, supports full registry sync,
and is designed for massive scale. Not appropriate for forge-metal's embedded single-node
use case — too much operational complexity.

### npm-proxy-cache

[npm-proxy-cache](https://github.com/runk/npm-proxy-cache) is a transparent HTTP caching
proxy. Requires `strict-ssl false` (MITM design). No auth, no publish, no package-level
access control, no air-gapped mode. Simpler than Verdaccio but less capable.

### Yarn offline mirror

Not a registry. Yarn can commit `.tgz` files to Git via `.yarn/cache/`. Guarantees every
commit is installable but bloats the Git repo. Complementary to Verdaccio, not a
replacement.

## Projects Using Verdaccio for CI

Major open-source projects that use Verdaccio as a **temporary test registry** (boot,
publish local build, install from it, verify):

- create-react-app
- Babel.js
- pnpm
- Storybook
- Angular CLI
- Docusaurus

These are all "publish + install" e2e test patterns. None of them use Verdaccio as a
persistent sealed cache the way forge-metal does. forge-metal's pattern is less common
but well-supported by Verdaccio's architecture.
