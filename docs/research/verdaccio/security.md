# Verdaccio Security

## CVE History

Verdaccio has a minimal CVE footprint. One formal advisory:

| ID | CVE | Severity | Fixed In | Description |
|----|-----|----------|----------|-------------|
| [GHSA-78j5-gcmf-vqc8](https://github.com/verdaccio/verdaccio/security/advisories/GHSA-78j5-gcmf-vqc8) | CVE-2019-14772 | High (GitHub) / Medium (NVD) | v3.12.0, v4.0.0 | XSS via malicious package content rendered in web UI |

Two additional XSS vulnerabilities from 2019 were patched in the same timeframe. One
Node.js-related medium-severity update in 2021. forge-metal's pin (v6.1.2) is not
affected by any known CVE.

The small CVE count is expected: Verdaccio is a simple proxy with a narrow attack surface.

## Attack Surface

### 1. Web UI XSS (mitigated in v4+)

The web UI renders package metadata (README, description). Malicious packages could inject
scripts. Fixed by sanitizing rendered content. **The web UI is not needed in CI** — disable
it if possible:

```yaml
web:
  enable: false
```

### 2. Uplink SSRF

Verdaccio will fetch any URL configured in `uplinks.*.url`. If an attacker can modify the
config, they can make Verdaccio issue requests to internal services. Mitigation: restrict
config file permissions, run as non-root (forge-metal already does this with the
`verdaccio` system user).

### 3. Dependency Confusion

If a private package name collides with a public package on npmjs, and `proxy: npmjs` is
active, Verdaccio resolves from the uplink. This is the classic dependency confusion
attack vector.

Mitigations:
- **Use scoped packages** (`@company/pkg`) for private packages
- **Remove proxy for private package patterns**:
  ```yaml
  packages:
    '@company/*':
      access: $authenticated
      publish: $authenticated
      # no proxy — private only
    '**':
      access: $all
      publish: $authenticated
      proxy: npmjs
  ```
- **Seal the registry** — once sealed, no uplink resolution occurs

### 4. Lifecycle Script Attacks

The primary supply-chain vector: malicious `preinstall` / `install` / `postinstall`
scripts in npm packages execute arbitrary code during `npm install`. This is not a
Verdaccio vulnerability — it's an npm ecosystem problem — but Verdaccio is the gate.

forge-metal's `scripts/security/inspect-tarballs.sh` checks for:
- Lifecycle scripts (`preinstall`, `install`, `postinstall`)
- Native build indicators (`binding.gyp`)
- Binary entries (`bin` field)

This is a good first-pass filter. More thorough approaches:
- `--ignore-scripts` flag during `npm install` (breaks packages that need postinstall)
- `socket.dev` or `snyk` scanning of the dependency tree before warmup

### 5. htpasswd Weakness

Default htpasswd uses `crypt` algorithm (DES-based, 8-character password limit). Upgrade:

```yaml
auth:
  htpasswd:
    file: /etc/verdaccio/htpasswd
    algorithm: bcrypt
    rounds: 10
    max_users: -1  # or 0 to disable self-registration
```

For forge-metal's sealed internal registry, authentication is less critical since the
registry is only accessible from the host and Firecracker VMs on the same machine. But
if the machine is ever multi-tenant, this matters.

## Hardened Config Template

```yaml
# Security-hardened Verdaccio config for sealed CI registry

storage: /var/lib/verdaccio/storage

auth:
  htpasswd:
    file: /etc/verdaccio/htpasswd
    algorithm: bcrypt
    rounds: 10
    max_users: 0  # No self-registration

security:
  api:
    jwt:
      sign:
        expiresIn: 7d
  web:
    sign:
      expiresIn: 1h

web:
  enable: false  # No web UI needed for CI

uplinks:
  npmjs:
    url: https://registry.npmjs.org/
    cache: true
    maxage: 30m
    strict_ssl: true

packages:
  '**':
    access: $all       # OK for single-machine behind firewall
    publish: $authenticated
    proxy: npmjs       # Removed when sealed

max_body_size: 100mb

listen: 127.0.0.1:4873  # Localhost only — VMs access via host network
```

## npm Provenance and Signatures

npm introduced package provenance (Sigstore-based attestations) in 2023. Verdaccio does
**not** verify or forward provenance attestations. When a package is cached, only the
tarball and metadata are stored — provenance data is lost.

This is a non-issue for the sealed registry pattern (the packages are vetted during
warmup, and the seal prevents any changes). But it means Verdaccio cannot participate in
npm's SLSA supply-chain security model.
