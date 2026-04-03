# OSV-Scanner (Google)

GitHub: [google/osv-scanner](https://github.com/google/osv-scanner) | 8,670 stars | Apache-2.0 | Go

Known vulnerability scanner backed by the OSV.dev database. Aggregates GHSA + NVD + OpenSSF Malicious Packages + 16 other sources. True offline mode with downloadable database zips. Single Go binary, no runtime dependencies.

## Offline Mode

Download 191MB npm database, scan without network:

```bash
# Download databases
osv-scanner scan --offline-vulnerabilities --download-offline-databases ./

# Scan offline
OSV_SCANNER_LOCAL_DB_CACHE_DIRECTORY=./db \
  osv-scanner scan --offline-vulnerabilities package-lock.json
```

Database stored at `{cache_dir}/osv-scanner/{ecosystem}/all.zip`. Each ecosystem is a separate zip from `gs://osv-vulnerabilities/{ECOSYSTEM}/all.zip`.

### Offline Limitations

- **Transitive dependency resolution disabled** for ecosystems that need it (Maven, not npm). npm lockfiles contain the full resolved tree, so this limitation does not affect forge-metal.
- **Commit-level scanning unsupported** offline.
- **No incremental sync** -- must re-download the full 191MB zip for updates.
- **Potentially slower** than online (range matching runs locally instead of server-side).

## npm Lockfile Support

| Format | Supported |
|--------|-----------|
| `package-lock.json` (v1/v2/v3) | Yes |
| `pnpm-lock.yaml` | Yes |
| `yarn.lock` (v1 and v2/Berry) | Yes |
| `bun.lock` (text format) | Yes |
| `bun.lockb` (binary) | No (Bun is migrating away from it) |
| `node_modules` (via container scanning) | Yes (V2, extracts `package.json` metadata) |

## Database Coverage

216,700 npm vulnerability entries. Primary source: GitHub Advisory Database (GHSA) -- the same upstream that powers `npm audit`. Additional sources: NVD CVEs, OpenSSF Malicious Packages database, Global Security Database, OSS-Fuzz findings.

**vs npm audit**: Same GHSA data at the core. OSV may surface additional findings from NVD or OpenSSF Malicious Packages that `npm audit` would miss. Critical advantage: OSV-Scanner works offline. `npm audit` phones home to npmjs.org and breaks in sealed registries.

**Real-world accuracy**: In comparative testing against the Shai-Hulud npm supply chain attacks, OSV-Scanner was the **only tool to detect both compromised packages** (ansi-regex 6.2.1, kill-port 2.0.2). Trivy detected zero. Grype detected them but with false positives. Source: [codecentric Shai-Hulud analysis](https://www.codecentric.de/en/knowledge-hub/blog/why-scanners-fail-in-practice-lessons-from-the-shai-hulud-attacks-on-npm).

**Scanner overlap**: Independent testing found only ~60-65% overlap between OSV-Scanner and Grype findings, suggesting they are complementary. OSV-Scanner caught transitive npm issues Grype missed; Grype caught container/base-image issues OSV missed.

## Guided Remediation

`osv-scanner fix` subcommand (v1.7.0+). Supports npm `package.json` + `package-lock.json`.

Two strategies:
- **In-place** (`--strategy=in-place`): replaces vulnerable versions in lockfile without changing `package.json`. Lower risk, fewer fixes.
- **Relock** (`--strategy=relock`): deletes lockfile, optionally relaxes `package.json` constraints, regenerates via `npm install --package-lock-only`. Higher risk, more fixes.

Key flags: `--min-severity=N`, `--ignore-dev`, `--no-introduce` (reject patches that add new vulns), `--data-source=native` (use npm registry directly).

Interactive mode: `osv-scanner fix --interactive -M package.json -L package-lock.json` (TUI).

Limitations: does not handle `overrides` field, `peerDependencies`, workspace relocking, or non-registry dependencies.

## Output Formats

| Format | Use Case |
|--------|----------|
| `table` | Human-readable (default) |
| `json` | Machine-parseable (CI pipelines) |
| `sarif` | GitHub Code Scanning integration |
| `markdown` | PR comments |
| `html` | Interactive dashboard (`--serve`) |
| `spdx-2-3` | SBOM output |
| `cyclonedx-1-4` / `cyclonedx-1-5` | SBOM output |

Exit codes: 0 (clean), 1 (vulns found), 127 (error), 128 (no packages found).

## SBOM Support

Consumes: SPDX v2.3, CycloneDX v1.4/v1.5 (auto-detected via `--sbom` flag).
Produces: SPDX v2.3, CycloneDX v1.4/v1.5 via `--format` flag.

## Limitations

1. **No malware detection** -- CVE/advisory-based only. Exception: ingests OpenSSF Malicious Packages database for cataloged malware.
2. **No reachability analysis for JavaScript** -- reports all vulns in tree, not just reachable ones. Go and Rust only. Commercial tools claim 70-90% alert reduction through reachability.
3. **No typosquatting detection**.
4. **Version lag** -- inherent delay between disclosure and database availability (hours to days).
5. **Packages without advisories are invisible** -- if no GHSA/CVE filed, OSV-Scanner cannot detect the vulnerability.
6. **No npm `overrides` awareness** in guided remediation.

## Performance

Single Go binary, negligible startup. Online mode: dominated by single batched API call (no rate limiting on osv.dev). Offline mode: slightly slower (local range matching against 191MB database). For 1000+ dependency lockfile: seconds, not minutes.

## Maintenance

- **Stars**: 8,670, Forks: 521
- **Release cadence**: monthly to bi-monthly
- **Active days**: 363/365 in past year
- **Monthly PRs**: 633 new/month, 4-day merge time
- **Contributors**: 41 quarterly active, 10 organizations
- **Concentration**: 2 contributors = 51%+ commits, 1 org (Google) = 51%+ contributions

OSV-Scanner is the CLI frontend for **OSV-SCALIBR**, the scanning library Google uses internally. V2 architecture deeply couples these projects. Apache-2.0 license and modular architecture mitigate Google graveyard risk.

## forge-metal Integration

```bash
# Weekly cron: refresh offline database
osv-scanner scan --offline-vulnerabilities --download-offline-databases /var/lib/osv-db/

# During warmup: scan each fixture's lockfile
OSV_SCANNER_LOCAL_DB_CACHE_DIRECTORY=/var/lib/osv-db \
  osv-scanner scan --offline-vulnerabilities --format json \
  /path/to/fixture/package-lock.json

# CI: exit-code-based gate
# Exit 1 = vulnerabilities found, fail the warmup
```

Replaces `npm audit` which cannot work in sealed registries.
