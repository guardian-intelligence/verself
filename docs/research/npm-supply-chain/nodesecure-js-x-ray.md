# NodeSecure js-x-ray

GitHub: [NodeSecure/js-x-ray](https://github.com/NodeSecure/js-x-ray) | 274 stars | MIT | TypeScript

AST-based JavaScript SAST scanner. Parses source into AST via Meriyah, traverses with scope-aware variable tracing. No regex, no Semgrep -- all detection through AST probes. Created by Thomas Gentilhomme (fraxken).

## Detection Capabilities

### Warning Types

**Critical:**
| Warning | Description |
|---------|-------------|
| `suspicious-file` | File with 10+ `encoded-literal` warnings |
| `obfuscated-code` (experimental) | High probability of code obfuscation (statistical meta-detection) |

**Warning:**
| Warning | Description |
|---------|-------------|
| `unsafe-import` | Cannot trace `require()` or `require.resolve()` |
| `unsafe-regex` | ReDoS-vulnerable regex (via safe-regex) |
| `unsafe-stmt` | `eval()` or `Function("")` constructor |
| `unsafe-command` (experimental) | Suspicious shell commands in `spawn()`/`exec()` |
| `short-identifiers` | Average identifier length < 1.5 chars (5+ identifiers) |
| `suspicious-literal` | Suspicious score exceeding threshold |
| `weak-crypto` | MD5, SHA1, or other weak algorithms |
| `shady-link` | Suspicious or malicious URLs |
| `serialize-environment` | `process.env` serialization (exfiltration) |
| `data-exfiltration` | Unauthorized sensitive data transfer |
| `sql-injection` | Potential SQL injection |
| `monkey-patch` | Built-in prototype modification |
| `prototype-pollution` | `__proto__` usage |

**Information:**
| Warning | Description |
|---------|-------------|
| `parsing-error` | AST parser failure |
| `encoded-literal` | Hex, Unicode, or Base64 encoded values |

### VariableTracer (v6.0+)

The key differentiator. Follows variable declarations and reassignments across scopes:

```javascript
// This evades regex/Semgrep but NOT js-x-ray:
const aA = Function.prototype.call;
const bB = require;
const crypto = aA.call(bB, bB, "crypto");
```

The tracer follows `require` aliased through `bB`, invoked through `Function.prototype.call` -- a pattern invisible to pattern matching. v6.0 reduced warnings from ~24,000 to ~5,000 across 500 popular npm packages by replacing heuristic detection with precise tracing.

### Probe Architecture

20 TypeScript probe modules, each registered to specific AST node types: `data-exfiltration`, `isArrayExpression`, `isBinaryExpression`, `isESMExport`, `isFetch`, `isImportDeclaration`, `isLiteral`, `isLiteralRegex`, `isMonkeyPatch`, `isPrototypePollution`, `isRandom`, `isRegexObject`, `isSerializeEnv`, `isSyncIO`, `isUnsafeCallee`, `isUnsafeCommand`, `isWeakCrypto`, `isWeakScrypt`, `log-usage`, `sql-injection`.

Custom probes can be added via `AstAnalyser({ customProbes, skipDefaultProbes })`.

## Obfuscation Detection

5 dedicated detectors plus Trojan Source:

| Obfuscator | Detection Method | Confidence |
|------------|-----------------|------------|
| **JSFuck** | Zero standard constructs + 5+ `!![]` patterns | High |
| **JJEncode** | 80%+ identifiers are `$`/`_` only | High |
| **obfuscator.io** | Multi-factor: DoubleUnaryExpression + deepBinaryExpression + hex identifier prefixes | High |
| **FreeJSObfuscator** | Consistent identifier prefix + pattern matching | Medium |
| **Trojan Source** | Unicode bidirectional text exploits (CVE-2021-42574) | High |

**Not explicitly detected**: aaencode (may be caught by `short-identifiers` or `obfuscated-code`). Custom hex/unicode encoding caught by `encoded-literal`. Eval chains caught by `isUnsafeCallee`. `Function("return this")` is specifically whitelisted as legitimate.

## The NodeSecure Ecosystem

For a complete supply chain solution:

| Package | Purpose | Required? |
|---------|---------|-----------|
| `@nodesecure/js-x-ray` | Core SAST engine | Yes |
| `@nodesecure/scanner` | Dependency tree walking, tarball extraction, orchestration | Yes |
| `@nodesecure/tarball` | npm tarball extraction and analysis | Bundled with scanner |
| `@nodesecure/flags` | 18 package-level security signals | Bundled with scanner |
| `@nodesecure/vulnera` | CVE detection (GHSA, Sonatype, OSV) | Optional |
| `@nodesecure/cli` | End-user CLI with web UI and PDF reports | Optional |
| `@nodesecure/rc` | Configuration (`ci`, `report`, `scanner` modes) | Optional |

### Scanner Flag System (18 flags per package)

`hasExternalCapacity` (network I/O), `hasWarnings` (js-x-ray findings), `hasNativeCode` (C++/Rust N-API), `hasCustomResolver` (git/file deps), `hasNoLicense`, `hasMultipleLicenses`, `hasMinifiedCode`, `isDeprecated`, `hasManyPublishers`, `hasScript` (install hooks), `hasIndirectDependencies`, `isGit`, `hasVulnerabilities`, `hasMissingOrUnusedDependency`, `isDead` (no update >1yr), `hasBannedFile` (sensitive files), `isOutdated`, `hasDuplicate`.

## Performance

Benchmarks from AMD EPYC 7763 (Feb 2026):

| File | Size | Time | Notes |
|------|------|------|-------|
| jscrush.js | 1 KB | ~1.3 ms | |
| event-stream.js | 3.8 KB | ~3-4 ms | Famous 2018 malware |
| modernize.js | 9.3 KB | ~10 ms | |
| kopiluwak.js | 15.5 KB | ~100 ms | Known malware |
| obfuscate.js | 89.6 KB | ~333 ms | Worst case |

Memory: 29 KB to 68 MB heap depending on file complexity. The scanner's `maxConcurrency` option (default: 8) parallelizes tarball processing.

Scaling estimate: 1000 typical npm package files (~5-10KB each) in ~5-15 seconds.

## API

```typescript
// Library usage (js-x-ray directly)
import { AstAnalyser } from "@nodesecure/js-x-ray";
const analyser = new AstAnalyser({ sensitivity: "conservative" });
const report = analyser.analyse(sourceCode);
// report: { warnings, flags, identifierLength, stringScore, executionTimeMs }

// Scanner usage (full dependency tree)
import * as scanner from "@nodesecure/scanner";
const result = await scanner.from("express", {
  registry: "http://localhost:4873",  // Verdaccio URL
  maxDepth: 4,
  maxConcurrency: 8,
  vulnerabilityStrategy: "github-advisory"
});

// Local project
const result = await scanner.workingDir("./my-project", {
  registry: "http://localhost:4873"
});
```

Supports private registries via `registry` option or `npm config`. `NODE_SECURE_TOKEN` env var for auth.

## False Positives

History of aggressive false positive reduction:
- v1.0: 90%+ of FPs from unpublished files (tests, coverage) in tarballs
- v6.0 (VariableTracer): warnings reduced from ~24,000 to ~5,000 across 500 packages. `unsafe-assign` warning eliminated entirely.
- Sensitivity modes: `"conservative"` (default) vs `"aggressive"`
- Optional warnings (`synchronous-io`, `log-usage`, `weak-scrypt`) are opt-in

No per-file inline suppression (`// nodesecure-disable` does not exist). Control is at API/config level.

## Comparison with GuardDog

| Pattern | js-x-ray | GuardDog |
|---------|----------|----------|
| Environment serialization | Yes | Yes |
| Obfuscation | 5 tool-specific detectors + statistical | YARA patterns |
| Shell execution | Yes | Yes |
| eval/Function | Yes | Yes (+ base64 taint) |
| Install scripts | No (scanner `hasScript` flag) | Yes |
| Data exfiltration | Yes | Yes |
| Steganography | No | Yes |
| DLL hijacking | No | Yes |
| Typosquatting | No | Yes |
| Manifest mismatch | No | Yes |
| ReDoS regex | Yes | No |
| SQL injection | Yes | No |
| Weak crypto | Yes | No |
| Prototype pollution | Yes | No |
| Monkey patching | Yes | No |
| Variable tracing | Yes (scope-aware) | No |

**Complementary, not redundant.** Only 3 detection categories overlap. 12+ are unique to one tool.

## Maintenance

- **Creator**: Thomas Gentilhomme (fraxken), 54% of 350 commits
- **Other contributors**: clemgbld (47), jean-michelet (12), fabnguess (12), bashlor (10)
- **Release cadence**: 7 releases in 5 weeks (Feb-Mar 2026). Extremely active.
- **Latest**: v15.0.0 (March 27, 2026)
- **Node.js requirement**: v22+ (scanner), v24+ (vulnera)
- **SLSA Level 3**, OSSF Scorecard badge
- Active GC pressure optimization (March 30, 2026 commit)

## Real-World Detections

- **colors.js / faker.js (Jan 2022)**: NodeSecure used during the Marak Squires incident to detect affected packages in dependency trees. [Blog post by fraxken](https://dev.to/fraxken/detect-marak-squires-packages-with-nodesecure-3lpo).
- **event-stream, forbes-skimmer, kopiluwak**: included as regression test samples in benchmark suite.
- Project motivated by analysis of **purescript-installer** and **event-stream** compromises.

No public record of js-x-ray being the first to discover unknown malware. Its value is in analysis and audit of dependency trees, not as a discovery engine.
