# forge-metal Verdaccio Gaps

Assessment of the current Verdaccio configuration in forge-metal, compared to best
practices from research.

## Current Config (ansible/roles/verdaccio/templates/config.yaml.j2)

```yaml
storage: /var/lib/verdaccio/storage
auth:
  htpasswd:
    file: /etc/verdaccio/htpasswd
uplinks:
  npmjs:
    url: https://registry.npmjs.org/
packages:
  '**':
    access: $all
    publish: $authenticated
    proxy: npmjs
listen: 0.0.0.0:4873
```

## Gaps

### 1. No `max_body_size` — will reject large publishes

Default is `10mb`. If you ever publish a large monorepo package or encounter a large
manifest, the request will be rejected with a confusing error. Add:

```yaml
max_body_size: 100mb
```

### 2. No `maxage` on uplink — wasteful re-fetches during warmup

Default `maxage` is `2m`. During warmup (`npm install` for all fixtures), Verdaccio
re-fetches metadata from npmjs every 2 minutes for the same packages. For a 10-minute
warmup, that's 5x redundant metadata fetches per package. Add:

```yaml
uplinks:
  npmjs:
    url: https://registry.npmjs.org/
    cache: true
    maxage: 30m
```

### 3. htpasswd algorithm not specified

Defaults to `crypt` (DES-based, 8-char password limit). Low risk for a localhost-only
registry, but trivially fixable:

```yaml
auth:
  htpasswd:
    file: /etc/verdaccio/htpasswd
    algorithm: bcrypt
    rounds: 10
    max_users: 0  # Disable self-registration
```

### 4. Listening on 0.0.0.0 — reachable from network

`listen: 0.0.0.0:4873` binds to all interfaces. Firecracker VMs access Verdaccio via
the host's TAP interface, so this may be necessary. But if the host has a public IP,
Verdaccio is exposed. Options:

- Bind to `127.0.0.1` + TAP bridge IP only
- Firewall port 4873 from external access (UFW/iptables)
- Accept the risk (registry is sealed, read-only, no sensitive data)

### 5. Web UI enabled by default

The web UI renders package READMEs and is the historical XSS attack surface. Not needed
for CI. Disable:

```yaml
web:
  enable: false
```

### 6. No `--no-audit` in CI workflows

`npm audit` bypasses the sealed registry and calls npmjs.org. If the network is down or
if you want deterministic CI, add `--no-audit` to all `npm install` commands in CI
workflows.

### 7. ~~Version pin is 6.1.2~~ — upgraded to 6.3.2

Upgraded in `ansible/group_vars/workers/main.yml`. Installed `verdaccio-security-filter@1.3.5`
for middleware-layer supply chain gating (package blocking, scope filtering, tarball gating).

**Note:** Verdaccio 6.x has broken `filter_metadata` plugin loading (verdaccio#5372, fix in
PR #5548 merged to master but never backported to 6.x). The `filters:` config section is
pre-declared so age/CVE/license checks activate automatically when upgrading to 7.x+.

## What's Already Good

- **Dedicated system user** (`verdaccio`, nologin shell) — correct privilege separation
- **CPU affinity** in systemd unit — prevents Verdaccio from competing with CI workloads
- **Seal/unseal scripts** — well-implemented, uses Python/YAML for safe config editing
- **Tarball inspection** — `inspect-tarballs.sh` checks lifecycle scripts and native builds
- **Local storage** — correct for single-node, no unnecessary S3 complexity
- **System-wide npmrc** — `/etc/npmrc` points all npm clients to local Verdaccio

## Recommended Config Template

Incorporating all gaps:

```yaml
storage: /var/lib/verdaccio/storage

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
listen: 0.0.0.0:{{ verdaccio_port }}
```
