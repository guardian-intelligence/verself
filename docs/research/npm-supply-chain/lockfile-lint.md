# lockfile-lint (Liran Tal)

GitHub: [lirantal/lockfile-lint](https://github.com/lirantal/lockfile-lint) | 848 stars | MIT | JavaScript

Lockfile policy linter. Validates that `package-lock.json` and `yarn.lock` entries point to expected registries, use HTTPS, have integrity hashes, and have consistent package names. The only dedicated open-source lockfile policy tool for npm/yarn.

## The Attack It Prevents

**Lockfile injection**: disclosed September 2019 by Liran Tal (Snyk). Presented at Black Hat Europe 2021.

The attack: attacker submits a PR modifying `package-lock.json`, changing `resolved` URLs to point to attacker-controlled servers and updating `integrity` hashes to match malicious tarballs. Reviewers rarely scrutinize machine-generated lockfile diffs. Once merged, `npm ci` trusts the lockfile absolutely -- it verifies the tarball matches the integrity hash, but if the attacker controls both `resolved` and `integrity`, `npm ci` is blind.

Source: [Snyk: Why npm lockfiles can be a security blindspot](https://snyk.io/blog/why-npm-lockfiles-can-be-a-security-blindspot-for-injecting-malicious-modules/)

No CVEs assigned for lockfile injection itself -- it's a design property of how lockfiles work.

## Validators

| CLI Flag | What It Checks |
|----------|---------------|
| `--validate-https` | Every `resolved` URL uses HTTPS. Mutually exclusive with `--allowed-schemes`. |
| `--allowed-hosts <hosts>` | Every `resolved` URL hostname matches whitelist. Built-in aliases: `npm`, `yarn`, `verdaccio`. |
| `--allowed-schemes <schemes>` | URI scheme whitelist (e.g., `https:`, `git+https:`). |
| `--allowed-urls <urls>` | Specific full URLs whitelisted. |
| `--validate-package-names` | `resolved` URL matches declared package name. Supports `--allowed-package-name-aliases`. |
| `--validate-integrity` | Integrity field exists and uses SHA-512. Supports `--integrity-exclude`. |
| `--empty-hostname` | Controls whether empty hostnames are permitted (for `github:` shorthand). Default: true. |

### Detection Matrix

| Vector | Detected? | Notes |
|--------|-----------|-------|
| Registry URL tampering | Yes | Primary defense via `--allowed-hosts` |
| Missing integrity hashes | Yes | `--validate-integrity` |
| Git protocol deps | Partial | `--allowed-schemes` can restrict to `git+https:` only |
| HTTP (non-HTTPS) URLs | Yes | `--validate-https` |
| File path dependencies | No | Entries without `resolved` field silently pass all URL validators |
| Package name confusion | Yes | `--validate-package-names` |

## Lockfile Format Support

| Format | Supported | Notes |
|--------|-----------|-------|
| `package-lock.json` (npm) | Yes (`--type npm`) | lockfileVersion 1 and 2. v3 unclear (omits `dependencies` field). |
| `npm-shrinkwrap.json` | Yes (`--type npm`) | Same parser. |
| `yarn.lock` v1 (classic) | Yes (`--type yarn`) | Primary supported format. |
| `yarn.lock` v2/Berry | Unclear | Different YAML-like format, not explicitly documented. |
| `pnpm-lock.yaml` | **No** | Deliberately unsupported. FAQ claims pnpm doesn't maintain tarball sources. |
| `bun.lockb` / `bun.lock` | **No** | Not mentioned in project. |

**Important**: The pnpm rationale was proven wrong by CVE-2025-69263 (GHSA-7vhp-vf5g-r2fw, CVSS 7.5, Jan 2026) -- HTTP tarball and git-hosted dependencies were stored without integrity hashes in pnpm <= 10.26.2.

## False Positives and Configuration

Known scenarios:
1. **GitHub shorthand** (`npm i owner/repo#branch`): `github:` prefix has no hostname, fails host validation. Fix: `--empty-hostname --allowed-schemes "github:"`.
2. **`git+https://` deps**: Flagged by `--validate-https` (scheme is `git+https:`, not `https:`). Fix: use `--allowed-schemes "https:" "git+https:"`.
3. **Local `file:` deps (monorepo workspaces)**: No `resolved` field, silently pass all URL validators. Zero coverage for workspace-linked packages.
4. **Scoped packages from private registries**: Must add all legitimate hosts to `--allowed-hosts`.
5. **Verdaccio-resolved packages**: `resolved` points to Verdaccio hostname, not npmjs.org. Use `--allowed-hosts your-verdaccio:4873`.
6. **`--validate-https` and `--allowed-schemes` are mutually exclusive** (issue #63).

Configuration via cosmiconfig: `package.json` key, `.lockfile-lintrc` (JSON/YAML), or `lockfile-lint.config.js/cjs/mjs`.

## CLI

```bash
# Basic: enforce npm registry + HTTPS
lockfile-lint --path package-lock.json --type npm \
  --allowed-hosts npm --validate-https

# With integrity and name validation
lockfile-lint --path package-lock.json --type npm \
  --allowed-hosts npm \
  --validate-https \
  --validate-integrity \
  --validate-package-names

# Verdaccio setup
lockfile-lint --path package-lock.json --type npm \
  --allowed-hosts your-verdaccio-host \
  --validate-https \
  --validate-integrity \
  --validate-package-names
```

Config file (`.lockfile-lintrc.json`):
```json
{
  "path": "package-lock.json",
  "type": "npm",
  "allowedHosts": ["npm"],
  "validateHttps": true,
  "validateIntegrity": true,
  "validatePackageNames": true
}
```

Output: `--format pretty` (default, with colors) or `--format plain` (no ANSI). No JSON output. Nonzero exit on failure.

## Value in Sealed Verdaccio Setup

In a sealed Verdaccio environment, lockfile-lint's primary value shifts from "prevent malicious injection" to **"enforce registry topology invariants"**:

1. **Registry drift** (`--allowed-hosts verdaccio-host`): catches when a developer commits a lockfile pointing at npmjs.org instead of Verdaccio because they forgot to configure their local npm.
2. **Integrity enforcement** (`--validate-integrity`): catches corruption or packages installed from cache without integrity fields (known npm bugs: npm/cli#4263, npm/cli#6301).
3. **HTTPS enforcement** (`--validate-https`): prevents accidental HTTP-only Verdaccio configs.
4. **Name verification** (`--validate-package-names`): catches name confusion even from local registry.

The primary lockfile injection attack (attacker changes `resolved` to their server) is already mitigated by the sealed registry -- `npm ci` would fail because the attacker's URL isn't Verdaccio. lockfile-lint adds defense-in-depth.

## Maintenance

- **Author**: Liran Tal, Director of Developer Relations at Snyk, Node.js Foundation Ecosystem Security working group, OpenJS Foundation Pathfinder for Security award.
- **Latest**: `lockfile-lint@5.0.0` (Jan 2025). `lockfile-lint-api@5.9.2`.
- **Release cadence**: irregular, maintenance-driven (3-6 month gaps).
- **Monorepo**: Turborepo with `packages/lockfile-lint` (CLI) and `packages/lockfile-lint-api` (library).
- Single maintainer. Stable, not growing.

## Ecosystem (Liran Tal)

| Tool | Purpose |
|------|---------|
| **npq** | Pre-install auditor: package age, typosquatting, email domain expiry, provenance, deprecation |
| **snync** | Dependency confusion defense (private name squatting on public registry) |
| **awesome-nodejs-security** | Curated Node.js security resources |

## Alternatives

`npm ci` / `yarn install --frozen-lockfile` / `pnpm install --frozen-lockfile` refuse to modify lockfiles and verify tarball hashes, but they do NOT verify that `resolved` URLs point to legitimate registries. If an attacker controls both `resolved` and `integrity`, built-in tools are blind.

`yarn --check-cache` (Berry only) refetches and verifies against both lockfile and cache -- the strongest built-in protection, but adds network overhead.

No other open-source tool provides equivalent lockfile policy enforcement.
