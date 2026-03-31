# Verdaccio Research Notes

Research notes for self-hosting a sealed npm registry with Verdaccio, focused on the
forge-metal CI pipeline where Firecracker VMs run `npm install` against a local registry.

## Context

forge-metal uses Verdaccio as a caching proxy that gets "sealed" (uplinks removed) before
CI runs. This eliminates network latency and npmjs.org availability as variables, and
prevents supply-chain attacks from reaching CI jobs. The key questions: how does caching
actually work at the source level, what breaks in air-gapped mode, and where are the
scaling walls.

## Method

Notes cite Verdaccio source code (GitHub `verdaccio/verdaccio` monorepo), official docs,
CVE databases, and GitHub issues. Version-specific claims target the 6.x stable line
unless noted otherwise.

## Notes

| File | Focus |
|------|-------|
| [architecture.md](architecture.md) | Storage internals, tarball vs metadata caching, on-disk layout |
| [sealed-registry.md](sealed-registry.md) | Air-gapped operation, warmup/seal pattern, what breaks offline |
| [security.md](security.md) | CVEs, attack surface, hardening config, tarball inspection |
| [performance.md](performance.md) | Concurrent CI access, tuning, scaling walls, alternatives |
| [gaps.md](gaps.md) | Assessment of forge-metal's current config, actionable gaps |

## Key Findings

1. **Tarball caching is the disk driver, not metadata.** Metadata is always cached.
   Tarballs are only cached when `cache: true` (the default). A Next.js monorepo's full
   dependency tree is 2-5 GB of tarballs.

2. **`npm audit` bypasses the sealed registry.** The audit middleware proxies requests to
   npmjs.org. In air-gapped mode, `npm audit` fails. Must use `--no-audit` in CI.

3. **No clustering support.** `.verdaccio-db.json` is the single-writer bottleneck.
   Single-node is the correct deployment — which is exactly what forge-metal does.

4. **`maxage` default of 2 minutes** means during warmup, metadata is re-fetched from
   npmjs every 2 minutes. Wasteful. Set to 30m+ during warmup.

5. **Only one real CVE** (CVE-2019-14772, XSS in web UI, fixed in v4). The attack surface
   is small because Verdaccio is a simple proxy.
