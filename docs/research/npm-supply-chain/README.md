# npm Supply Chain Security Scanners

Evaluation of self-hosted npm package security scanners for integration into forge-metal's sealed Verdaccio registry workflow. All tools must run fully offline with no SaaS dependencies.

## Context

forge-metal runs a sealed Verdaccio registry: packages are fetched from npmjs.org during a warmup phase, scanned, then the registry is sealed for air-gapped CI in Firecracker microVMs. Scanning happens between warmup and seal -- this is the security gate.

## Tool Evaluation

| Document | Tool | Category |
|----------|------|----------|
| [guarddog.md](guarddog.md) | DataDog GuardDog | Malware detection (static heuristics) |
| [nodesecure-js-x-ray.md](nodesecure-js-x-ray.md) | NodeSecure js-x-ray | Code analysis (AST-based SAST) |
| [packj.md](packj.md) | Ossillate Packj | Malware detection (static + dynamic) |
| [osv-scanner.md](osv-scanner.md) | Google OSV-Scanner | Known vulnerability scanning (CVE) |
| [lockfile-lint.md](lockfile-lint.md) | Liran Tal lockfile-lint | Lockfile integrity enforcement |
| [analysis-matrix.md](analysis-matrix.md) | Comparative analysis | Decision matrix and recommendation |

## Attack Vectors Covered

```
                          GuardDog  js-x-ray  Packj  OSV-Scanner  lockfile-lint
Obfuscated malware           +         ++       -        -             -
Install script attacks       ++        -        ++       -             -
Data exfiltration patterns   ++        ++       +        -             -
Typosquatting                ++        -        +        -             -
Known CVEs                   -         -        +        ++            -
Lockfile injection           -         -        -        -             ++
Registry URL tampering       -         -        -        -             ++
Metadata anomalies           ++        -        ++       -             -
Runtime behavior             -         -        ++       -             -

++  = primary strength
+   = partial coverage
-   = not covered
```

## Recommended Stack

1. **GuardDog** -- scan every tarball entering Verdaccio for malware patterns
2. **js-x-ray** -- deep JavaScript AST analysis for obfuscation and evasion
3. **OSV-Scanner** -- known CVE matching with offline database
4. **lockfile-lint** -- enforce registry topology invariants in CI

Packj is excluded: unmaintained (2-year gaps), closed-source sandbox blob, not viable for production.
