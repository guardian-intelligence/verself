# forge-metal Verdaccio Gaps

Assessment of the current Verdaccio configuration in forge-metal, compared to best
practices from research.

## Current Config (ansible/roles/verdaccio/templates/config.yaml.j2)

```yaml
storage: {{ verdaccio_storage_path | default('/var/lib/verdaccio/storage') }}

auth:
  htpasswd:
    file: /etc/verdaccio/htpasswd
    algorithm: bcrypt
    rounds: 10
    max_users: 0

web:
  enable: false

uplinks:
  npmjs:
    url: https://registry.npmjs.org/
    cache: true
    maxage: 30m
    strict_ssl: true

packages:
  '**':
    access: $all
    publish: $authenticated
    proxy: npmjs

max_body_size: 100mb
listen: {{ services.verdaccio.host }}:{{ services.verdaccio.port }}
```

## Supply Chain Enforcement

Verdaccio 6.x has broken `filter_metadata` plugin loading (verdaccio#5372, fix in
7.x). The `filters:` config section is pre-declared so age/CVE/license checks activate
automatically when upgrading to 7.x+. Until then, the `filters:` section is inert.

**The Go scan pipeline is the actual enforcement layer.** `forge-metal ci scan-registry`
runs four scanners (age, guarddog, jsxray, osv-scanner) against cached tarballs in the
ZFS staging dataset. The `mirror-update.yml` playbook gates ZFS snapshot promotion on
scan pass — production Verdaccio is never exposed to unscanned packages.

The middleware layer (`verdaccio-security-filter`) handles blockedPatterns and
blockedScopes for runtime request blocking. It does not process packageAge, cveCheck,
or licenses — those keys were removed from the middleware config since they had no
effect.

## Resolved Gaps

1. **max_body_size** — set to 100mb
2. **maxage on uplink** — set to 30m
3. **htpasswd algorithm** — bcrypt with 10 rounds, self-registration disabled
4. **Web UI** — disabled
5. **Version pinning** — 6.3.2, verdaccio-security-filter@1.3.5

## Remaining Gaps

### 1. Listening on 0.0.0.0 — reachable from network

`listen: 0.0.0.0:4873` binds to all interfaces. Firecracker VMs access Verdaccio via
the host's TAP interface, so this may be necessary. Mitigated by nftables rules
restricting external access to port 4873.

### 2. blockedPatterns is empty

The middleware's blocklist (`blockedPatterns: []`, `blockedScopes: []`) has no entries.
Typosquat patterns and known-malicious package names are candidates. Low-priority since
the sealed registry only contains packages that passed the scan pipeline.

### 3. No `--no-audit` in CI workflows

`npm audit` bypasses the sealed registry and calls npmjs.org. If the network is down or
if you want deterministic CI, add `--no-audit` to all `npm install` commands in CI
workflows.

## What's Already Good

- **Dedicated system user** (`verdaccio`, nologin shell) — correct privilege separation
- **CPU affinity** in systemd unit — prevents Verdaccio from competing with CI workloads
- **ZFS clone-scan-promote workflow** — atomic, rollback-safe mirror updates
- **Four-scanner gate** — age, guarddog (malware), jsxray (AST analysis), osv (CVEs)
- **Local storage** — correct for single-node, no unnecessary S3 complexity
- **System-wide npmrc** — `/etc/npmrc` points all npm clients to local Verdaccio
