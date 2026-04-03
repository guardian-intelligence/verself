# Analysis Matrix

Comparative evaluation of 5 self-hosted npm supply chain security scanners for forge-metal's sealed Verdaccio workflow.

## Decision Matrix

| Dimension | GuardDog | js-x-ray | Packj | OSV-Scanner | lockfile-lint |
|-----------|----------|----------|-------|-------------|---------------|
| **Detection depth** | Medium | Deep | Medium | Shallow (CVE-only) | Narrow (lockfile-only) |
| **False positive rate** | Medium-High | Low-Medium | Very High | Low | Very Low |
| **Signal quality** | High (real catches) | High (AST precision) | Low (noisy defaults) | High (advisory-backed) | High (binary pass/fail) |
| **Performance** | Slow (Semgrep startup) | Fast (~1-10ms/file) | Slow (strace overhead) | Fast (Go binary) | Trivial |
| **Offline capability** | Source scan: yes. Metadata: no | Full offline | No | Yes (`--offline`) | Full offline |
| **Integration effort** | Low (pip install, CLI) | Medium (Node.js library) | Medium (Docker recommended) | Low (single Go binary) | Low (npx, CLI) |
| **Maintenance** | Active (DataDog, 2 FTEs) | Active (7 releases in 5 weeks) | Unmaintained (~2yr gaps) | Active (Google, 41 contributors) | Maintenance mode (single maintainer) |
| **License** | Apache-2.0 | MIT | AGPL-3.0 | Apache-2.0 | MIT |
| **Real-world catches** | 270+ malicious packages | event-stream, colors.js | 339 (academic study) | Shai-Hulud npm attacks | N/A (policy tool) |
| **Bus factor** | 2 (DataDog engineers) | 1.5 (Thomas Gentilhomme) | 1 (Ashish Bijlani) | Medium (Google team) | 1 (Liran Tal) |

## Detailed Scoring (1-5, higher is better for forge-metal)

| Criterion | Weight | GuardDog | js-x-ray | Packj | OSV-Scanner | lockfile-lint |
|-----------|--------|----------|----------|-------|-------------|---------------|
| Malware detection | 5 | 4 | 3 | 3 | 1 | 0 |
| Known vuln coverage | 3 | 0 | 0 | 1 | 5 | 0 |
| Lockfile integrity | 2 | 0 | 0 | 0 | 0 | 5 |
| Self-hosted / offline | 5 | 3 | 5 | 1 | 4 | 5 |
| False positive mgmt | 4 | 2 | 4 | 1 | 4 | 5 |
| Integration simplicity | 3 | 4 | 3 | 2 | 5 | 5 |
| Maintenance confidence | 4 | 5 | 4 | 1 | 4 | 3 |
| **Weighted total** | | **82** | **82** | **34** | **78** | **62** |

Scoring: multiply criterion score (0-5) by weight, sum across criteria. Max possible = 130.

## Per-Tool Verdicts

### GuardDog -- ADOPT

**Role:** Primary malware scanner for all tarballs entering Verdaccio during warmup.

Strengths:
- 18 npm-specific rules (9 source + 9 metadata) catch the attack patterns that matter: install scripts, data exfiltration, obfuscation, steganography, typosquatting, DLL hijacking
- Operational track record: caught DPRK-aligned npm malware (Stressed Pungsan), MUT-8694/1692/6149/4831 campaigns
- DataDog invests real engineering: 2 FTEs, monthly releases, OpenSSF accepted
- Semgrep + YARA engine means rules are transparent YAML, auditable, extensible

Risks:
- `npm-obfuscation` rule fires on legitimate minified code (every bundled package)
- Non-deterministic results reported (issue #613) -- same package, different results
- Hardcoded directory exclusions (`dist/`, `build/`, `.github/`) are a known evasion vector (issue #665)
- Metadata analysis requires network (queries npm registry, WHOIS)
- Semgrep subprocess startup adds ~1-3s per package

Mitigations:
- `--exclude-rules npm-obfuscation` for packages known to ship minified (or accept the noise during warmup-only scanning)
- Run metadata analysis during warmup when network is available
- The directory exclusion evasion is concerning but requires attacker sophistication beyond typical npm malware

### js-x-ray -- ADOPT

**Role:** Deep JavaScript code analysis, complementary to GuardDog. Run on extracted package sources after GuardDog flags or on all packages for defense in depth.

Strengths:
- AST-based with VariableTracer -- follows variable reassignments across scopes, catches aliased `require()` and `Function.prototype.call` chains that regex/Semgrep cannot
- 5 dedicated obfuscation tool detectors (JSFuck, JJEncode, obfuscator.io, FreeJSObfuscator, Trojan Source) plus statistical meta-detection
- Fast: ~1-10ms per file, ~333ms worst case for 89KB obfuscated file
- Library-first API (`@nodesecure/scanner`) supports custom registry URL -- direct Verdaccio integration
- MIT license, SLSA Level 3
- Full NodeSecure ecosystem: scanner handles tarball fetch/extract/analyze, vulnera adds CVE checking, flags system provides 18 package-level signals

Risks:
- Bus factor: Thomas Gentilhomme = 54% of commits
- No metadata-level detection (typosquatting, manifest mismatch) -- must pair with GuardDog
- No published evidence of first-discovering unknown malware (strength is analysis, not discovery)
- Requires Node.js v22+ (scanner) / v24+ (vulnera)

Why both GuardDog AND js-x-ray:
- GuardDog catches install scripts, metadata anomalies, typosquatting, steganography, DLL hijacking -- attack surface signals
- js-x-ray catches deep code obfuscation, variable aliasing evasion, prototype pollution, monkey patching -- code-level signals
- Only 3 detection categories overlap (env serialization, shady links, unsafe eval). 12+ categories are unique to one tool
- Combined: broader coverage than either alone, at the cost of two runtimes (Python + Node.js)

### Packj -- REJECT

**Role:** N/A -- not recommended for production use.

The concept is sound (static + dynamic + metadata analysis, strace sandbox for install-time behavior). The academic paper (NDSS 2021, 339 malware reported) validates the approach. But the implementation is a research prototype:

- Effectively unmaintained: single developer, 2-year gaps between meaningful code changes
- Closed-source `sandbox.o` binary blob -- unauditable security-critical component
- x86-only sandbox
- >80% of npm top-1000 packages would trigger flags with default settings (very noisy)
- packj.dev service appears to be down
- Cannot work fully offline (metadata analysis requires registry queries)
- AGPL-3.0 license (not a legal problem for CLI use, but signals an academic project, not production tooling)

If dynamic analysis is needed in the future, OpenSSF's `package-analysis` (Apache-2.0, actively maintained, Google-backed) is a better foundation.

### OSV-Scanner -- ADOPT

**Role:** Known vulnerability scanning with offline database. Replace `npm audit` (which phones home to npmjs.org and breaks when sealed).

Strengths:
- True offline mode: download 191MB npm database, scan with `--offline-vulnerabilities`
- OSV aggregates GHSA + NVD + OpenSSF Malicious Packages -- broader than `npm audit` (GHSA only)
- 216,700 npm vulnerability entries
- Single Go binary, no runtime dependencies
- Guided remediation (`osv-scanner fix`) for npm lockfiles -- unique among OSS tools
- Multiple output formats: JSON, SARIF, CycloneDX, SPDX
- Only tool that detected both Shai-Hulud compromised npm packages in comparative testing
- Apache-2.0, deeply embedded in Google's OSS security infrastructure

Risks:
- No malware detection (CVE-only, plus OpenSSF Malicious Packages catalog for known malware)
- No reachability analysis for JavaScript (Go and Rust only) -- reports all vulns in tree, not just reachable ones
- ~60-65% overlap with Grype findings (adding Grype would increase coverage)
- Google graveyard risk (mitigated: project underpins OSV-SCALIBR, used internally at Google)

For forge-metal:
- Run during warmup with `--offline-vulnerabilities` on all fixture lockfiles
- Refresh the offline database weekly via cron on the host
- npm lockfiles contain the full resolved tree, so the offline transitive resolution limitation (affects Maven) does not apply

### lockfile-lint -- ADOPT

**Role:** Enforce registry topology invariants. Run in CI pre-merge.

Strengths:
- 7 validators covering registry URL tampering, integrity hash enforcement, HTTPS, package name verification
- In a sealed Verdaccio setup, the primary value is **configuration correctness**: catches when a developer commits a lockfile pointing at npmjs.org instead of Verdaccio
- Trivial performance (JSON parsing + URL checks)
- Zero false positives when configured correctly
- MIT license

Limitations:
- npm and yarn lockfiles only (no pnpm, no bun)
- `file:` dependencies silently pass all validators (monorepo workspace packages are invisible)
- Single maintainer (Liran Tal), maintenance-mode release cadence
- No JSON output format

For forge-metal:
- Configure `--allowed-hosts` to your Verdaccio hostname
- Add `--validate-integrity --validate-https --validate-package-names`
- Run as a pre-merge check in Forgejo Actions

## Integration Architecture

```
Warmup Phase (online)                          Seal Phase (offline)
─────────────────────                          ────────────────────
                                               
  npm install                                  CI Job (Firecracker VM)
       │                                              │
       ▼                                              ▼
  Verdaccio ◄── npmjs.org                      npm ci --no-audit
       │                                              │
       ▼                                              ▼
  ┌─────────────────────┐                      lockfile-lint
  │  For each tarball:   │                     (allowed-hosts = verdaccio)
  │                      │                            │
  │  1. guarddog scan    │                            ▼
  │  2. js-x-ray analyse │                     PASS / FAIL
  │  3. osv-scanner      │
  │     --offline        │
  └─────────────────────┘
           │
           ▼
     PASS → seal-registry.sh seal
     FAIL → abort, report findings
```

## What This Stack Does Not Cover

| Gap | Mitigation | Priority |
|-----|------------|----------|
| Runtime behavior analysis | OpenSSF package-analysis if needed later | Low (static catches 90%+ of npm malware) |
| Minimum release age quarantine | Custom script checking npm registry `time` field during warmup | Medium |
| npm provenance / Sigstore verification | Custom script verifying attestation bundles | Medium |
| Dependency pinning policy (ban `*`, `latest`) | Custom linter on `package.json` | Low |
| Reproducible install hashing | Hash Verdaccio storage dir, compare across warms | Low |
| pnpm/bun lockfile validation | lockfile-lint does not support these; use `--frozen-lockfile` flags | Low (fixture repos use npm/pnpm/bun lockfiles, not lockfile-lint's problem) |
