# GuardDog (DataDog)

GitHub: [DataDog/guarddog](https://github.com/DataDog/guarddog) | 1,040 stars | Apache-2.0 | Python

Static malware detection for npm packages via Semgrep rules + YARA signatures + metadata heuristics. Maintained by DataDog Security Labs. OpenSSF sandbox project.

## Detection Rules

### Source Code (Semgrep) -- 9 npm rules

| Rule | Detection |
|------|-----------|
| `npm-exec-base64` | Taint: `Buffer.from()`/`atob()` flowing into `eval()`/`new Function()` |
| `npm-obfuscation` | `while(!![])` loops, `_0x` hex identifiers, `String.fromCharCode`, JSFuck (`[\[\]\(\)\+\!]{10,}`), entropy on string arrays, eval packers, trailing whitespace code |
| `npm-install-script` | `preinstall`/`postinstall`/`install` in package.json. Allowlist: `npx patch-package`, `prisma generate`, `husky install` |
| `npm-exfiltrate-sensitive-data` | Taint: `process.env`, `os.homedir()`, `readFile('/etc/passwd')`, `.aws/credentials`, `.ssh/id_rsa` flowing to `http.request`, `axios`, `node-fetch`, Firebase |
| `npm-serialize-environment` | `JSON.stringify(process.env)` including bracket-notation variants |
| `npm-api-obfuscation` | 28 patterns: `module[fn]()`, `Reflect.get()`, `Object.getOwnPropertyDescriptor()` with `.call()`/`.apply()`/`.bind()` variants |
| `npm-silent-process-execution` | `child_process.spawn/exec` with `{ detached: true, stdio: 'ignore' }` |
| `npm-dll-hijacking` | `rundll32`, `regsvr32`, `mshta` invocation; `WriteProcessMemory`/`CreateRemoteThread`/`LoadLibraryA` injection; phantom DLL writes |
| `npm-steganography` | Taint: image file refs (`.jpg`, `.png`, `.gif`) flowing to `eval()` or `steggy.reveal` |

Cross-ecosystem `shady-links.yml` also runs on npm packages.

### Metadata -- 9 npm rules

| Rule | Detection |
|------|-----------|
| `bundled_binary` | Compiled binaries in package |
| `deceptive_author` | Misleading author information |
| `direct_url_dependency` | Dependencies pointing to URLs instead of registry |
| `empty_information` | Missing description, README, metadata |
| `npm_metadata_mismatch` | Inconsistencies between package.json and registry |
| `potentially_compromised_email_domain` | Maintainer email on compromised domains |
| `release_zero` | Version 0.x packages |
| `typosquatting` | Edit-distance against top packages list |
| `unclaimed_maintainer_email_domain` | Unregistered/expired maintainer email domains |

## Real-World Catches

DataDog processes ~20,000 new package releases daily. Over 270 malicious packages identified, cataloged in [malicious-software-packages-dataset](https://github.com/DataDog/malicious-software-packages-dataset).

| Date | Campaign | What GuardDog Caught |
|------|----------|---------------------|
| Jul 2024 | Stressed Pungsan (DPRK) | `harthat-hash`, `harthat-api`: `npm-install-script` + `shady-links` + `npm-dll-hijacking` fired. Packages removed within hours. |
| Oct 2024 | MUT-8694 | `larpexodus` (PyPI) triggered download-executable detection. Same binary in npm packages delivering Blank Grabber infostealer. |
| Q1 2025 | MUT-1692 | `argus3-test`: malicious postinstall creating socket to C2. Same actor later compromised Rspack via stolen npm token. |
| Q2 2025 | MUT-6149 | `grayavatar@1.0.2`: multi-stage nested script invocation, obfuscated infostealer. |
| Oct 2025 | MUT-4831 | `custom-tg-bot-plan@1.0.1`: 5 indicators, trojanized npm delivering Vidar infostealer. |

Sources: [Stressed Pungsan](https://securitylabs.datadoghq.com/articles/stressed-pungsan-dprk-aligned-threat-actor-leverages-npm-for-initial-access/), [MUT-8694](https://securitylabs.datadoghq.com/articles/mut-8964-an-npm-and-pypi-malicious-campaign-targeting-windows-users/), [Q1 2025](https://securitylabs.datadoghq.com/articles/2025-q1-threat-roundup/), [Q2 2025](https://securitylabs.datadoghq.com/articles/2025-q2-threat-roundup/), [MUT-4831](https://securitylabs.datadoghq.com/articles/mut-4831-trojanized-npm-packages-vidar/)

## Known Issues

### False Positives

- **Issue #654**: `npm-api-obfuscation` fires on legitimate `api[f](...args)` patterns. Minified code intentionally flagged but clear non-obfuscated code also triggers.
- **Issue #630**: `suspicious_passwd_access_linux` YARA rule matches `/etc/passwd` in `@types/node` JSDoc comments.
- **Issue #613**: Non-deterministic results. Same package scanned twice gives different findings (1 vs 0). Root cause: Semgrep config argument ordering.
- **Issue #703**: Git URL format variations cause spurious `npm_metadata_mismatch` alerts.

No per-package allowlist. Available controls: `--exclude-rules <rule>` to skip entire rules, `--rules <rule>` to run only specific rules. SARIF output supports GitHub Code Scanning dismiss-alert for triaging.

### Evasion Vectors

**Issue #665 (unresolved)**: Semgrep hardcodes directory exclusions -- `dist/`, `build/`, `.github/`, `test/`, `tests/`, `venv/`, `migrations/`. Malicious code placed in these directories completely evades detection. Moving identical code from root to `dist/` changes verdict from "found" to "clean". Not configurable.

**Issue #508 (fixed)**: A `.semgrepignore` file with `*` in package root caused Semgrep to skip all files. Fixed with `--disable-nosem` flag.

## Architecture

- **Semgrep**: invoked as subprocess (not library). Requires `semgrep >= 1.147.0` (installed as Python dependency). All npm rules passed in single invocation via multiple `--config` flags. Flags: `--json --quiet --disable-nosem --no-git-ignore --timeout=10`.
- **YARA**: `yara-python ^4.5.1`, compiled C library with Python bindings, in-process. Extension-based exclusions skip `.json`, `.txt`, `.md`.
- **Metadata**: pure Python. Queries `https://registry.npmjs.org/{name}` for metadata checks. Typosquatting uses edit-distance. Email domain checks use `python-whois`. Repository integrity uses `pygit2`.

### Offline Capability

| Mode | Offline? |
|------|----------|
| Source code analysis (Semgrep + YARA) | Yes -- fully local |
| Metadata analysis | No -- queries npm registry, WHOIS servers |
| Remote package scan | No -- downloads from npm registry |
| Local directory/tarball scan | Yes |

### Performance

No published benchmarks. Bottleneck is Semgrep subprocess startup (~1-3s per package based on typical Semgrep overhead). YARA is fast (compiled, in-process). `GUARDDOG_PARALLELISM` env var controls concurrency for `verify` command (multi-package). Within a single package, Semgrep and YARA execute sequentially.

Default limits: `--max-target-bytes=10000000` (10MB), `--timeout=10` (seconds per file).

## CLI

```bash
# Install
pip install guarddog

# Scan local tarball
guarddog npm scan ./package-1.0.0.tgz

# Scan local directory
guarddog npm scan ./my-package

# Scan remote package
guarddog npm scan express --version 4.18.2

# Verify all deps in lockfile
guarddog npm verify package-lock.json --output-format sarif --exit-non-zero-on-finding

# List rules
guarddog npm list-rules

# Exclude noisy rules
guarddog npm scan ./pkg --exclude-rules npm-obfuscation
```

Output formats: default (human-readable), `--output-format json` (scan), `--output-format sarif` (verify). Exit code 0 by default regardless of findings; use `--exit-non-zero-on-finding` for CI.

## Maintenance

- **Contributors**: 30 total. Primary: sobregosodd (485 commits), christophetd (251 commits, co-creator).
- **Release cadence**: ~monthly (v2.9.0 Feb 2026, v2.8.x Jan 2026, v2.7.x Jan/Oct 2025).
- **Telemetry**: none. No OpenTelemetry dependencies, no phone-home code.
- **DataDog's supply-chain-firewall** project wraps `npm`/`pip` commands, checking against GuardDog + OSV.dev before allowing installation.
