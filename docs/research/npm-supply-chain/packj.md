# Packj (Ossillate)

GitHub: [ossillate-inc/packj](https://github.com/ossillate-inc/packj) | 686 stars | AGPL-3.0 | Python

Static + dynamic malware detection with strace-based install-time sandbox. Academic spin-off from Georgia Tech (NDSS 2021 paper). Checks 40+ risk attributes across metadata, source code, and runtime behavior.

**Verdict: REJECT for production use.** Sound concept, unmaintained implementation.

## Detection Capabilities

### Metadata Analysis
Package age, author email validity, expired email domains, 2FA status, source repo activity (stars/forks/commits/contributors), download count (<1,000 threshold), version count (<5 threshold), dependency count (>50 threshold), release gap (>180 days), release-yank ratio (>50%), missing README/homepage, forked repo detection.

### Static Code Analysis
Network API usage, filesystem API usage, process spawning, obfuscation patterns (`eval`, `getattr`), runtime code generation, environment variable access, SSH key access, install scripts, binary executables, decode+exec sequences.

### Dynamic Analysis (strace)
Install package under strace, monitor `open()` (file access), `connect()` (network), `fork()`/`exec()` (process spawning). Records all filesystem modifications, network connections, DNS queries, IPv4/IPv6 attempts.

## Dynamic Analysis Architecture

The sandbox uses strace to intercept syscalls at the ptrace level. Critical detail: strace alone only logs syscalls. Packj ships a **closed-source binary blob `sandbox.o`** (compiled to `libsbox.so`) that hooks via `LD_PRELOAD` to perform argument inspection and rewriting. The developers state this is "NOT to implement security by obscurity, but to avoid easy copy-and-reuse scenarios" -- will be open-sourced eventually.

- **x86 only** -- no ARM/non-x86 binaries (GitHub issue #98)
- **Docker recommended** for safety (you are executing potentially malicious code)
- Does not require Docker for basic strace operation, but should only run in containers
- Filesystem isolation: intercepts and redirects writes to isolated layer
- Network: blocks all by default, allowlist for known registries

**Coverage limitation**: install-time behavior only. The developers note "93.9% of malicious packages use at least one install script," but runtime malware that activates on `require()` is not caught.

## Academic Backing

**Paper**: "Towards Measuring Supply Chain Attacks on Package Managers for Interpreted Languages"
- Authors: Ruian Duan, Omar Alrawi, Ranjita Pai Kasturi, Ryan Elder, Brendan Saltaformaggio, Wenke Lee
- Venue: NDSS 2021
- [arXiv:2002.01139](https://arxiv.org/abs/2002.01139)

Results: 339 malware reported across npm/PyPI/RubyGems, 278 confirmed (82% confirmation rate), 3 CVEs assigned. 3 packages had 100K+ downloads each.

Related: Ashish Bijlani (Packj creator) holds PhD in Cybersecurity from Georgia Tech, 5 US patents. Prior work includes OSSPolice (CCS 2017) and a lightweight filesystem sandboxing framework (APSys 2018) directly relevant to Packj's sandbox design.

## Why REJECT

1. **Unmaintained**: Single developer (Ashish Bijlani), ~2-year gaps between meaningful code changes. Last substantive development early 2024. March 2026 activity was README-only.
2. **Closed-source security-critical component**: `sandbox.o` binary blob is unauditable.
3. **Very high false positive rate**: Default settings flag `browserify` for "generates new code at runtime," "reads files and dirs," "forks or exits OS processes." Expect >80% of npm top-1000 to trigger flags.
4. **Not offline-capable**: Metadata analysis requires registry queries. packj.dev service appears down.
5. **x86-only sandbox**: No ARM support.
6. **AGPL-3.0**: Not a legal blocker for CLI use (running unmodified Packj as subprocess does not trigger copyleft), but signals academic project, not production tooling.
7. **No incremental scanning**: Must re-analyze all packages, no diff-based workflow.

## If Dynamic Analysis Is Needed Later

OpenSSF's [package-analysis](https://github.com/ossf/package-analysis) (871 stars, Apache-2.0, Go) is a better foundation:
- Detonates packages inside gVisor sandbox
- Captures strace + network packet data
- Actively maintained by OpenSSF
- Docker-compose self-hostable
- Higher operational overhead than a CLI, but designed for continuous monitoring

## CLI Reference

```bash
# Audit packages
packj audit -p npm:lodash npm:express

# Audit from dependency file
packj audit -f npm:package.json

# With dynamic analysis
packj audit -p npm:lodash --trace

# Sandbox install
packj sandbox npm install <package>
```

Docker: `docker pull ossillate/packj:latest`

Output: JSON files with `PASS`/`RISK`/`ALERT` categorization, `undesirable` array, `vulnerable` array, detected permissions.
