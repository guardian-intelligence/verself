# Nix Language Advanced Patterns, Determinate Nix vs Lix, and Fleet Management

Three topics that fill gaps in the existing research:

1. **Nix language advanced patterns** — non-obvious language features every flake author hits
2. **Determinate Nix vs Lix vs CppNix** — detailed comparison of the three Nix distributions
3. **Multi-host fleet management** — deploy-rs, colmena, nixos-anywhere, disko, and the nix-copy baseline

---

## Part 1: Nix Language Advanced Patterns

### 1.1 `with` Statement Pitfalls

#### Scoping Semantics (Precise)

A `with e1; e2` expression introduces the attributes of the set `e1` as implicit definitions into the lexical scope of `e2`. The key distinction in the Nix reference manual is between **explicit** and **implicit** definitions:

- **Explicit definitions**: created by `let`, `rec`, and function literal parameters
- **Implicit definitions**: created only by `with`

**The fundamental rule**: an explicit definition can replace a definition of any type, but an implicit definition can only replace another implicit definition. This means `let` bindings always win over `with`:

```nix
let stdenv = "my-custom-stdenv"; in
with pkgs;
stdenv   # => "my-custom-stdenv" — let binding wins
```

Inner `with` beats outer `with`:

```nix
with { a = "outer"; };
with { a = "inner"; };
a  # => "inner"
```

#### Why nixpkgs Bans `with pkgs;` at Top-Level in Package Files

The nixpkgs style guide explicitly forbids `with pkgs;` at file scope in package files. Three reasons:

1. **Static analysis failure**: nix-lsp, deadnix, and other tools cannot determine which names are in scope without evaluating the file. Type checkers and linters must skip entire files that use `with` at file scope.

2. **Ambiguous provenance**: with multiple `with` statements (a common pattern in complex expressions), there's no way to tell which attrset a name comes from. `with lib; with pkgs; curl` — is `curl` from `lib` or `pkgs`?

3. **Shadowing footgun**: an attribute added to `pkgs` in the future could silently shadow a local binding. The `with` expression makes this failure invisible until runtime.

**The nixpkgs-approved alternative**:

```nix
# Instead of:
with pkgs; [ curl jq ripgrep ]

# Use inherit:
{ inherit (pkgs) curl jq ripgrep; }
# or list directly:
[ pkgs.curl pkgs.jq pkgs.ripgrep ]
```

#### `with` in `let...in` Blocks

`with` inside a `let` block is more acceptable because the scope is narrow:

```nix
let
  myPkg = with pkgs; stdenv.mkDerivation {
    # with pkgs here is fine — scope is limited to this attrset
    buildInputs = [ curl openssl ];
  };
in myPkg
```

The risk is still present but localized. The real trap is `with` at the top of a file before a `rec {}` attrset — there the `with`-imported names interact with `rec`'s own shadowing.

---

### 1.2 `inherit (x) a b` vs `inherit a b`

These are syntactically similar but semantically different:

```nix
# Without parens: copies from the CURRENT scope
let a = 1; b = 2; in
{ inherit a b; }
# equivalent to: { a = a; b = b; }

# With parens: copies from a SPECIFIC attrset
{ inherit (pkgs) stdenv curl; }
# equivalent to: { stdenv = pkgs.stdenv; curl = pkgs.curl; }
```

#### Error Mode Differences

```nix
# inherit without parens — fails with "undefined variable 'a'"
# if 'a' is not in scope
let x = 1; in { inherit a; }   # Error: undefined variable 'a'

# inherit with parens — fails with "attribute 'a' missing"
# if the attribute doesn't exist in the attrset
{ inherit (pkgs) nonexistent; }  # Error: attribute 'nonexistent' missing in set
```

The distinction matters for error messages during debugging. The parenthesized form's error names the attrset; the unparenthesized form's error names the variable.

#### Common Mistake Pattern

```nix
# WRONG: inherit stdenv from current scope (must already be bound)
{ stdenv, ... }: { inherit stdenv; }   # works only if stdenv is a param

# WRONG: inherit (pkgs) when pkgs is not in scope
{ lib, ... }: { inherit (pkgs) stdenv; }   # Error: undefined variable 'pkgs'

# RIGHT: inherit from the correct scope
{ pkgs, ... }: { inherit (pkgs) stdenv curl; }
```

---

### 1.3 `rec {}` vs `let...in {}` for Self-Referential Attrsets

#### `rec {}` Semantics

`rec { a = 1; b = a + 1; }` adds all attribute names as implicit definitions into the scope of all attribute values. This enables mutual references but has a critical pitfall:

**The shadowing bug**:

```nix
let name = "outer-name"; in
rec {
  name = "inner-name";   # shadows outer 'name' everywhere in this rec
  drv = stdenv.mkDerivation {
    name = name;   # refers to THIS rec's 'name', not the outer let
    # This is fine HERE — but what if mkDerivation uses 'name' internally?
  };
}
```

A more dangerous example from nixpkgs practice:

```nix
# DANGEROUS: 'stdenv' inside rec {} shadows any outer 'stdenv'
rec {
  stdenv = pkgs.stdenv;
  myPkg = stdenv.mkDerivation { ... };  # fine

  # but if someone adds:
  stdenv = pkgs.stdenv.override { ... };
  # now myPkg silently picks up the new stdenv — or infinite recursion
  # if the override references stdenv itself
}
```

**Infinite recursion via name shadowing**:

```nix
let a = 1; in
rec { a = a; }   # 'a' on the RHS refers to the rec's own 'a', not let's 'a'
                 # => infinite recursion
```

#### `let...in {}` as the Safe Alternative

nixpkgs prefers `let...in {}` for self-referential structures:

```nix
# SAFE: explicit self-reference, no implicit scope pollution
let
  self = {
    a = 1;
    b = self.a + 1;   # explicitly qualified reference
    c = self.b * 2;
  };
in self
```

The `let` pattern is verbose but predictable. The outer name `self` does not interfere with any inner attribute names.

**When `rec` is actually fine**: small attrsets with no external names in scope, and when you're certain no attribute name conflicts with outer bindings. Example:

```nix
# Fine — small, no shadowing risk
versions = rec {
  major = 3;
  minor = 14;
  patch = 1;
  full = "${toString major}.${toString minor}.${toString patch}";
};
```

---

### 1.4 String Context and `noContext` Operations

#### What String Context Encodes

In Nix, every string is secretly a pair: a character sequence plus a **string context** — an unordered set of context elements. String context is how Nix tracks build-time dependencies through string interpolation without requiring explicit dependency declarations.

Three types of context elements:

| Type | Encoding | What triggers it |
|------|----------|-----------------|
| **Constant path** | `{ path = true; }` | `builtins.storePath "/nix/store/..."` |
| **Output reference** | `{ outputs = ["out"]; }` | Interpolating `"${drv}"` or `"${drv.outPath}"` |
| **Derivation deep** | `{ allOutputs = true; }` | `builtins.outputOf drv "out"` |

Example: interpolating a derivation into a string adds that derivation's output path as a context element. This is what prevents you from using a derivation path as a URL without first building it.

#### Introspection Functions

```nix
# builtins.getContext: returns the context as an attrset
builtins.getContext "${pkgs.hello}"
# => { "/nix/store/<hash>-hello-<ver>.drv" = { outputs = ["out"]; }; }

# builtins.appendContext: adds context to a string
builtins.appendContext "some-string" {
  "/nix/store/<hash>-foo.drv" = { outputs = ["out"]; };
}

# builtins.hasContext: test for non-empty context
builtins.hasContext "plain string"   # => false
builtins.hasContext "${pkgs.hello}"  # => true

# builtins.unsafeDiscardStringContext: strips ALL context
builtins.unsafeDiscardStringContext "${pkgs.hello}"
# => "/nix/store/<hash>-hello-<ver>" (string, no deps tracked)
```

#### `builtins.unsafeDiscardStringContext` Use Cases

The "unsafe" label means Nix's normal dependency tracking guarantee is broken. Legitimate uses:

1. **`lib.strings.sanitizeDerivationName`**: strips context before applying `builtins.replaceStrings`, because `replaceStrings` can't handle strings with context. The derivation name is purely informational, not a real path.

2. **Deriving file names from store paths**: when you want to embed a hash into a file name without creating a build dependency on the source derivation.

3. **Avoiding IFD**: sometimes you have a derivation path in a string and want to compute a derived string from it without triggering import-from-derivation.

**The trap**: discarding context means Nix will no longer ensure the referenced path exists before the current derivation builds. If your builder depends on the discarded path, the build will fail at runtime (not eval time) with a missing path error.

---

### 1.5 `builtins.derivation` Raw Interface

`stdenv.mkDerivation` is a large wrapper around `builtins.derivation`. Understanding the raw interface clarifies what mkDerivation actually does.

#### Mandatory Attributes

| Attribute | Type | Purpose |
|-----------|------|---------|
| `name` | string | Symbolic name; becomes part of the store path (`/nix/store/<hash>-<name>`) |
| `system` | string | Target build platform (e.g., `"x86_64-linux"`); use `builtins.currentSystem` for native |
| `builder` | path or string | The executable that performs the build; commonly `pkgs.bash` or `"/bin/sh"` |

#### Optional Attributes

| Attribute | Type | Default | Purpose |
|-----------|------|---------|---------|
| `args` | list of strings | `[]` | Arguments passed to `builder` |
| `outputs` | list of strings | `["out"]` | Named output paths; each becomes an env var during build |
| `__structuredAttrs` | bool | `false` | When true, serializes all attrs to JSON (see below) |
| `passAsFile` | list of strings | `[]` | Names of attrs passed via temp files (for large strings) |
| `impureEnvVars` | list of strings | `[]` | Env vars passed through (FODs only) |
| `requiredSystemFeatures` | list of strings | `[]` | Required machine capabilities (e.g., `"kvm"`) |
| `allowedReferences` | null or list | `null` | Whitelist of direct runtime dependencies |
| `disallowedReferences` | list | `[]` | Blacklist of forbidden runtime dependencies |

Every other attribute becomes an environment variable in the builder's environment, with automatic type coercion:
- `string` → passed as-is
- `int` → decimal string representation
- `bool` → `"1"` for true, `""` for false
- `path` → the path is copied to the store; the store path string is passed
- `derivation` → the derivation is listed as a dependency; its `outPath` is passed as a string

#### `__structuredAttrs = true` and the JSON Env File

When `__structuredAttrs = true`, all attributes are serialized into a JSON file rather than individual environment variables. The builder receives two environment variables:

- `NIX_ATTRS_JSON_FILE`: path to a JSON file with all attributes
- `NIX_ATTRS_SH_FILE`: path to a Bash-sourceable file with shell variable assignments

This is critical for list and attrset attributes: without `__structuredAttrs`, lists are serialized as space-separated strings (breaking on items with spaces), and attrsets cannot be passed at all. With `__structuredAttrs`, they're serialized as proper JSON arrays and objects.

Example in a builder script:
```bash
source "$NIX_ATTRS_SH_FILE"
# Now $buildInputs is a bash array, $name is a string, etc.
```

`outputChecks` (for CA derivation output validation) only works with `__structuredAttrs = true`:

```nix
builtins.derivation {
  __structuredAttrs = true;
  outputChecks.out = {
    maxSize = 10 * 1024 * 1024;  # 10 MB
    allowedReferences = [];       # no runtime deps
    ignoreSelfRefs = true;
  };
  # ...
}
```

#### `passAsFile`

For very large string attributes that exceed environment variable size limits (Linux default: 128KB total env block):

```nix
builtins.derivation {
  passAsFile = [ "bigScript" ];
  bigScript = ''
    #!/bin/bash
    # ... thousands of lines ...
  '';
  # builder receives $bigScriptPath instead of $bigScript
}
```

---

### 1.6 `builtins.placeholder`

Returns a synthetic store path placeholder string for a named output. Used inside derivation setup to reference an output's path before the content-addressed path is known.

```nix
builtins.placeholder "out"   # => "/1rz4g4znpzjwh1xymhjpm42vipw92pr73vdgl6xs1hycac8kf2n9"
builtins.placeholder "dev"   # => "/xnff…" (different synthetic path)
```

This is primarily used in `mkDerivation`'s setup hooks and in CA derivation implementations. The placeholder strings are replaced by the real output paths at build time. From a flake author's perspective, you rarely call this directly — but it explains why environment variables like `$dev` and `$out` in builder scripts contain valid-looking store paths before the build completes.

---

### 1.7 `assert` Statement

#### Syntax and Behavior

```nix
assert <condition>; <expression>
```

If `<condition>` evaluates to `true`, returns `<expression>`. If `false`, aborts evaluation with an `AssertionError` and a backtrace. The error message is generic unless you use `lib.assertMsg`.

```nix
# Raw assert — unhelpful error message
assert pkgs.stdenv.isLinux; pkgs.myLinuxPackage

# lib.assertMsg — human-readable error
assert lib.assertMsg pkgs.stdenv.isLinux "myPackage requires Linux";
pkgs.myLinuxPackage

# lib.assertOneOf — validates against a list
assert lib.assertOneOf "format" format ["elf" "pe" "macho"];
buildPackage { inherit format; }
```

#### What `assert` Does NOT Do in NixOS Modules

**Critical**: `assert` inside a NixOS module config block does NOT generate a proper error. Module evaluation defers config expressions, and an `assert` failure inside `config` will produce a confusing traceback rather than a useful user-facing message.

**The correct pattern for modules**:

```nix
# WRONG in module config:
config = {
  assertions = []; # use this instead
  assert config.services.foo.enable -> config.services.bar.enable;  # will work but ugly
};

# RIGHT — use the assertions option:
config = {
  assertions = [
    {
      assertion = config.services.foo.enable -> config.services.bar.enable;
      message = "services.foo requires services.bar to be enabled";
    }
  ];
  warnings = lib.optional (config.services.foo.deprecated) "services.foo is deprecated";
};
```

The `assertions` option is processed by the NixOS module system to produce clean, actionable error messages during `nixos-rebuild`.

---

### 1.8 `builtins.tryEval`

#### What It Catches

```nix
builtins.tryEval expr
# => { success = true; value = <result>; }
# or { success = false; value = false; }
```

`tryEval` performs **shallow evaluation** (WHNF) and catches errors from:
- `throw "message"` — explicit throws
- `assert false; ...` — assertion failures
- `builtins.abort "message"` — DOES NOT catch this (abort is uncatchable)

**What `tryEval` does NOT catch**:
- Type errors: `1 + "a"` — throws a type error, not catchable
- Missing attribute: `{}.a` — not catchable
- `builtins.abort` — always kills evaluation
- Infinite recursion — may be caught in some versions, behavior is inconsistent

The `success = false` case always returns `{ success = false; value = false; }` — the `value` field is always `false` on failure, not the error message.

#### Practical Use: Feature Detection

```nix
# Test if nixpkgs has a given package (it might not on all systems)
let attempt = builtins.tryEval pkgs.somePackage;
in if attempt.success then attempt.value else pkgs.fallbackPackage

# Test a flag that might not be set
let hasNewFeature = (builtins.tryEval pkgs.config.newFeature).success;
in lib.optionalAttrs hasNewFeature { ... }
```

#### `builtins.tryEval` vs Module Assertions

In flake code outside modules, `tryEval` can wrap risky expressions. Inside the module system, use `assertions` instead — `tryEval` inside a module config block is an anti-pattern because it hides evaluation errors that should be surfaced.

---

### 1.9 `__functor`: Making Attrsets Callable

An attrset with a `__functor` attribute is callable — it can be used as a function:

```nix
{
  __functor = self: arg: self.value + arg;
  value = 10;
}
# => this attrset can be called: mySet 5 => 15
```

The `self` parameter receives the attrset itself, enabling stateful callables.

#### `makeOverridable` Uses `__functor`

`lib.makeOverridable` (covered in [fods-ifd-and-internals.md](fods-ifd-and-internals.md)) returns an attrset, not a function. It can still be called because it has `__functor`:

```nix
# Conceptual implementation of makeOverridable result:
{
  __functor = self: args: self.__inner (self.__args // args);
  __inner = f;
  __args = originalArgs;
  override = newArgs: makeOverridable f (originalArgs // newArgs);
  # ... plus the original derivation attributes
}
```

This is why `pkg.override { ... }` works even though `pkg` looks like a derivation attrset — the `override` attribute is a function, but `pkg` itself is callable via `__functor` in some implementations.

#### `lib.makeExtensible`

`lib.makeExtensible` uses `__functor` to create an attrset that can be extended by passing it an overlay function:

```nix
let
  base = lib.makeExtensible (self: {
    a = 1;
    b = self.a + 1;
  });
in base.extend (self: super: { a = 10; })
# => { a = 10; b = 11; }  (b picks up new a via self)
```

The `extend` method internally uses `lib.fix` over an overlay chain. `lib.makeExtensible` is the basis for nixpkgs's `pkgs.extend` (adding overlays to an existing package set instance).

#### Using `__functor` in Flake Outputs

```nix
# Callable module that also has metadata attributes
nixosModules.myModule = {
  __functor = _self: import ./modules/my-module.nix;
  _documentation = "See docs/my-module.md";
};
```

This is uncommon — most flake module exports are plain functions or paths — but legal.

---

### 1.10 `builtins.addErrorContext`

Wraps an expression to add a context string to any error messages that propagate through it. The context string is prepended to error traces, making deep evaluation failures debuggable.

```nix
builtins.addErrorContext "while evaluating myPackage" expr
# If expr throws, the error message becomes:
# "while evaluating myPackage\n  <original error>"
```

#### `lib.addErrorContext`

nixpkgs provides `lib.addErrorContext` as an alias (same semantics):

```nix
lib.addErrorContext "while computing derivation name for ${name}" (
  lib.sanitizeDerivationName rawName
)
```

#### Where nixpkgs Uses It

Throughout `lib/modules.nix` and `lib/customisation.nix`, `addErrorContext` wraps module evaluation steps:

```nix
# From lib/modules.nix (simplified):
builtins.addErrorContext "while evaluating the module argument '${key}' in '${filename}'"
  (args.${key})
```

This is why NixOS module errors print a chain of context strings showing exactly which module, which option, and which evaluation step triggered the failure.

#### For Flake Authors

When writing complex helper functions that evaluate many things, wrap the entry point:

```nix
myHelper = args:
  builtins.addErrorContext "in myHelper (called with ${builtins.toJSON args})"
  (actualImplementation args);
```

The `builtins.toJSON args` call deep-forces `args` which can itself throw — if that's a risk, use a safer label:

```nix
builtins.addErrorContext "in myHelper"
  (actualImplementation args)
```

---

## Part 2: Determinate Nix vs Lix vs CppNix

### 2.1 The Three Nix Distributions

As of 2026, there are three active Nix implementations/distributions:

| Distribution | Maintainer | Based on | Status |
|-------------|------------|----------|--------|
| **CppNix** (upstream) | NixOS/Nix team | C++ codebase | Reference implementation, experimental flakes |
| **Determinate Nix** | Determinate Systems | Fork of CppNix | Commercial, flakes stable, lazy trees enabled |
| **Lix** | Community (lix.systems) | Fork of CppNix 2.18 | Community-governed, correctness-focused |

### 2.2 Determinate Nix: What It Is and What It Changes

Determinate Nix is a downstream fork/distribution of NixOS/nix maintained at `github.com/DeterminateSystems/nix-src`. It is "continuously rebased against" upstream while adding proprietary enhancements. The Determinate Nix distribution has two components:

1. **Determinate Nix CLI** — the customized `nix` binary
2. **Determinate Nixd** — a background daemon handling certificate management, garbage collection policy, and enterprise configuration

#### Default Feature Differences vs CppNix

| Feature | CppNix | Determinate Nix |
|---------|--------|-----------------|
| Flakes | Experimental (opt-in) | Stable, enabled by default |
| Lazy trees | Not merged (PR #13225 rejected) | Shipped in 3.5.2, default in 3.8.0 |
| Parallel eval | Not available | Enabled (claimed 50%+ speedup) |
| `nix-command` | Experimental (opt-in) | Stable, enabled by default |
| FlakeHub integration | None | Native (`fh` CLI, `flakehub:` URLs) |
| Automatic GC | Manual only | Intelligent automatic GC via Nixd |
| WebAssembly eval | Not available | Available |
| Flake schemas | Not available | Available |

#### Lazy Trees in Determinate Nix

Lazy trees (PR #13225 for upstream, shipped in Determinate 3.5.2) avoids materializing entire flake inputs to disk before evaluation. Instead of copying all of nixpkgs (~433 MB) into the Nix store before evaluating, only the files actually accessed during evaluation are fetched:

- Without lazy trees: ~10.8s wall time, ~433 MB disk for a nixpkgs eval
- With lazy trees: ~3.5s wall time, ~11 MB disk for the same eval (~3x faster, ~40x less disk)

The upstream Nix team rejected PR #13225 because virtual store paths use randomized hashes, introducing non-determinism into language semantics: two evaluations of the same expression can see different virtual path strings, violating Nix's referential transparency guarantees.

#### FlakeHub Integration

FlakeHub is Determinate Systems' flake registry and binary cache service. It provides:

**Semver-style pinning for flake inputs**:
```nix
# In flake.nix inputs:
inputs.nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.1.*.tar.gz";
# *.tar.gz = "any patch release of 0.1.x"
```

**FlakeHub Cache**: a binary cache substitute. Instead of rebuilding or fetching from cache.nixos.org, FlakeHub Cache serves pre-built closures for flakes published to FlakeHub. In GitHub Actions, this replaces the common `magic-nix-cache-action` pattern.

**`fh` CLI** (separate tool, `github.com/DeterminateSystems/fh`):
```bash
fh add nixpkgs          # adds nixpkgs to current flake with FlakeHub URL
fh add github:owner/repo  # adds a GitHub flake via FlakeHub
fh list                 # lists available flakes on FlakeHub
fh search <query>       # search FlakeHub registry
```

#### Determinate Nix Installer

The Determinate Nix installer (`github.com/DeterminateSystems/nix-installer`) differs from the official `nix.sh` installer:

| Property | Official installer | Determinate installer |
|----------|-------------------|----------------------|
| Language | Bash | Rust |
| Uninstall | Manual, difficult | `nix-installer uninstall` (uses receipt) |
| `receipt.json` | No | Yes — records every change made |
| macOS volume | Manual setup | Automatic APFS volume creation |
| Partial failure | Leaves system dirty | Atomic: rolls back on failure |
| Multi-user | Optional | Always multi-user |
| Flakes | Requires manual nix.conf | Enabled by default |

`receipt.json` is stored at `/nix/receipt.json` and records every filesystem change, user/group creation, and config file modification. The uninstaller reads this to reverse all changes precisely.

The installer for `--init none` (Docker/container environments) skips systemd service creation while still setting up the Nix store.

### 2.3 Lix: The Community Fork

Lix (`lix.systems`, `git.lix.systems/lix-project/lix`) is a community-maintained fork of CppNix 2.18, started in 2024 in response to the NixOS Foundation governance crisis.

#### Why Lix Was Created

The 2024 NixOS Foundation governance crisis (Eelco Dolstra's board resignation, the moderation team controversy, the RFC process breakdown) produced a community schism. A group of contributors felt that:

1. CppNix development was too slow and opaque
2. The upstream project was captured by commercial interests (Determinate Systems)
3. Community governance structures had failed

Lix forked from CppNix 2.18 with the goal of being a community-governed alternative that treats flakes as de-facto stable.

#### Key Differences from CppNix

| Property | CppNix | Lix |
|----------|--------|-----|
| Governance | NixOS Foundation + commercial contributors | Community-governed (BDFL-less) |
| Flakes status | Experimental | Treated as stable (but still behind flag for compatibility) |
| Rust components | Minimal | More components ported to Rust |
| Release cadence | Slower, larger releases | More frequent, smaller releases |
| Error messages | Standard | Improved, more actionable |
| MesonBuild | Migrating (Nix 2.26) | Already complete |
| Experimental features | Conservative | More experimental features available |

#### Key Differences from Determinate Nix

| Property | Determinate Nix | Lix |
|----------|----------------|-----|
| Commercial backing | Yes (Determinate Systems) | No (community-funded) |
| Lazy trees | Yes | No (not merged) |
| Parallel eval | Yes | No |
| FlakeHub | Yes (native) | No |
| FlakeHub Cache | Yes | No |
| NixOS module | Via nixd daemon | `lix-module` flake overlay |
| Philosophy | Feature addition | Correctness and stability first |

#### Installing Lix on NixOS

Lix provides a NixOS module via `lix-module`:

```nix
# flake.nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    lix-module = {
      url = "https://git.lix.systems/lix-project/nixos-lix-module/archive/main.tar.gz";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { nixpkgs, lix-module, ... }: {
    nixosConfigurations.my-host = nixpkgs.lib.nixosSystem {
      modules = [
        lix-module.nixosModules.default  # replaces nix package with Lix
        ./configuration.nix
      ];
    };
  };
}
```

The `lix-module` overlay replaces the `nix` package in `nixpkgs` with the Lix binary, and applies Lix-specific NixOS configuration. The Nix daemon is replaced transparently.

#### Lix's Stance on `--extra-experimental-features`

Lix maintains the experimental features flag system for backward compatibility, but its position is that flakes are effectively stable and should be treated as such. Lix does not enable flakes by default (to avoid breaking existing configurations), but its documentation recommends enabling them and implies they will not be broken by Lix releases.

### 2.4 CppNix Governance

CppNix (the upstream `NixOS/nix` repository) governance issues:

- **RFC 49 anomaly**: the original flakes RFC was withdrawn after implementation was already merged (2021), creating a precedent where implementation predated RFC acceptance
- **Eelco Dolstra** (flakes author) resigned from the NixOS Foundation board in 2024
- **RFC process breakdown**: multiple high-priority RFCs (RFC 134, 136) stalled in committee; community confidence in the RFC process declined
- **NixOS Foundation response**: established a new governance structure; the "CppNix" name was coined by the community to distinguish the upstream C++ implementation from forks
- **Meson migration**: RFC 132 (Meson build system) was accepted and implemented in Nix 2.26, representing one of the few successfully completed structural RFCs

---

## Part 3: Multi-Host Fleet Management

Managing a fleet of NixOS machines with flakes requires tooling on top of `nix copy` + `ssh`. The ecosystem has converged on several tools with different tradeoffs.

### 3.1 `nixosConfigurations` Multi-Host Patterns

#### Basic Multi-Host Flake

```nix
# flake.nix
{
  outputs = { nixpkgs, ... }@inputs: {
    nixosConfigurations = {
      node-01 = nixpkgs.lib.nixosSystem {
        system = "x86_64-linux";
        specialArgs = { inherit inputs; fleetVars = { domain = "example.com"; }; };
        modules = [
          ./hosts/node-01/hardware-configuration.nix
          ./modules/common.nix
          ./modules/clickhouse.nix
        ];
      };
      node-02 = nixpkgs.lib.nixosSystem {
        system = "x86_64-linux";
        specialArgs = { inherit inputs; fleetVars = { domain = "example.com"; }; };
        modules = [
          ./hosts/node-02/hardware-configuration.nix
          ./modules/common.nix
          ./modules/forgejo.nix
        ];
      };
    };
  };
}
```

#### `specialArgs` Pattern

`specialArgs` passes extra arguments to modules beyond the standard `{config, pkgs, lib, ...}`. Common uses:

- Fleet-wide variables (domain, environment, region)
- Flake inputs (so modules can reference other flake outputs)
- Secrets references (ages, sops-nix key paths)

```nix
# In a module, specialArgs are available as extra args:
{ config, pkgs, lib, inputs, fleetVars, ... }:
{
  services.caddy.virtualHosts."${fleetVars.domain}".extraConfig = "...";
}
```

#### Sharing Modules Across Hosts

```nix
# modules/common.nix — imported by all hosts
{ config, pkgs, lib, fleetVars, ... }:
{
  # Common configuration: users, SSH keys, base packages, NTP, etc.
  users.users.deploy = {
    isNormalUser = true;
    openssh.authorizedKeys.keys = [ "ssh-ed25519 AAAA..." ];
  };
  nix.settings.experimental-features = [ "nix-command" "flakes" ];
}

# hosts/node-01/hardware-configuration.nix — generated by nixos-generate-config
# imports = [];
# boot.initrd.availableKernelModules = [...];
# fileSystems."/" = { device = "/dev/nvme0n1p1"; fsType = "ext4"; };
```

#### Per-Host Hardware Configs

Hardware configs are generated on the target machine:
```bash
nixos-generate-config --show-hardware-config > hosts/node-01/hardware-configuration.nix
```

Or with nixos-anywhere's `--generate-hardware-config` flag (see §3.4).

### 3.2 `deploy-rs`: Multi-Profile Deployment

`deploy-rs` (`github.com/serokell/deploy-rs`) is a Rust tool for deploying flake-based NixOS configurations. Key distinction from simple `nixos-rebuild switch --target-host`: deploy-rs supports multiple **profiles** per host, where a profile is any derivation with an activation script.

#### Flake Output Schema

```nix
# flake.nix
{
  outputs = { self, nixpkgs, deploy-rs, ... }: {
    nixosConfigurations.my-host = nixpkgs.lib.nixosSystem { ... };

    deploy.nodes.my-host = {
      hostname = "192.168.1.100";
      fastConnection = false;   # false = copy closures locally (slower but works without substituters)
      profilesOrder = [ "system" "app" ];  # order of profile activation
      sshOpts = [ "-p" "2222" ];

      profiles.system = {
        sshUser = "root";
        user = "root";
        path = deploy-rs.lib.x86_64-linux.activate.nixos self.nixosConfigurations.my-host;
        activationTimeout = 300;  # seconds
        confirmTimeout = 30;      # seconds to wait for magic rollback confirmation
      };

      profiles.app = {
        sshUser = "deploy";
        user = "myapp";
        path = deploy-rs.lib.x86_64-linux.activate.custom self.packages.x86_64-linux.myApp
          "systemctl restart myapp.service";
        autoRollback = true;
      };
    };

    # Required: deploy-rs needs to check its own derivation
    checks = builtins.mapAttrs
      (_system: deployLib: deployLib.deployChecks self.deploy)
      deploy-rs.lib;
  };
}
```

#### Activation Modes

| Mode | Function | What it does |
|------|----------|-------------|
| `activate.nixos` | NixOS system | Runs `nixos-rebuild switch` equivalent |
| `activate.home-manager` | Home Manager | Runs `home-manager switch` |
| `activate.nix-env` | nix-env profile | Activates a nix-env profile |
| `activate.custom drv cmd` | Arbitrary | Runs `cmd` after installing `drv` to profile |

The activate.nixos path (available as `activate.nixos self.nixosConfigurations.hostname`) wraps `nixos-rebuild switch` logic including running activation scripts.

#### Magic Rollback Mechanism

The magic rollback is deploy-rs's primary safety feature:

1. Deploy-rs connects to the target host
2. Activates the new profile
3. Waits `confirmTimeout` seconds for a re-connection to succeed
4. If the re-connection fails (SSH unreachable), the target node's activation script auto-triggers a rollback to the previous profile generation

This prevents "I accidentally blocked SSH in my firewall rule" scenarios. Disable with `magicRollback = false` or `--no-magic-rollback` CLI flag.

#### CLI Commands

```bash
# Deploy all nodes and all profiles
deploy '.#'

# Deploy specific node
deploy '.#my-host'

# Deploy specific node + profile
deploy '.#my-host.system'

# Override hostname
deploy '.#my-host' -- --hostname 10.0.0.5

# Skip flake checks (nix flake check)
deploy '.#' --skip-checks

# Dry run (build but don't activate)
deploy '.#' --dry-activate

# No magic rollback
deploy '.#' --no-magic-rollback

# No auto rollback on activation failure
deploy '.#' --no-auto-rollback
```

### 3.3 `colmena`: Tag-Based Fleet Deployment

Colmena (`github.com/zhaofengli/colmena`) is a stateless NixOS deployment tool written in Rust, modeled after NixOps and morph. It wraps `nix-instantiate` and `nix copy` with fleet-aware parallelism and tag-based targeting.

#### Hive Configuration

Colmena reads a `hive.nix` (or flake `nixosConfigurations` with a colmena-specific wrapper):

```nix
# hive.nix (traditional style)
{
  meta = {
    nixpkgs = import <nixpkgs> {};
    nodeNixpkgs = {
      # Per-node nixpkgs overrides (e.g., different branch for canary)
      canary-node = import (builtins.fetchTarball "...") {};
    };
  };

  defaults = { pkgs, ... }: {
    # Imported by ALL hosts
    environment.systemPackages = [ pkgs.htop ];
    nix.settings.experimental-features = [ "nix-command" "flakes" ];
    deployment.targetUser = "root";
  };

  node-01 = { name, nodes, ... }: {
    deployment = {
      targetHost = "192.168.1.101";
      targetPort = 22;
      tags = [ "web" "production" ];
      buildOnTarget = false;        # build locally, push closure
      allowLocalDeployment = false;
      replaceUnknownProfiles = true; # apply even if current profile unknown
    };
    imports = [ ./hosts/node-01/configuration.nix ];
    # Cross-reference other nodes:
    # services.nginx.upstreams.app.servers = lib.mapAttrs' (name: node: {
    #   name = "${node.config.networking.hostName}:8080";
    # }) (lib.filterAttrs (_: n: builtins.elem "app" n.config.deployment.tags) nodes);
  };
}
```

#### Flake Integration

```nix
# flake.nix
{
  outputs = { nixpkgs, colmena, ... }: {
    colmena = {
      meta = {
        nixpkgs = import nixpkgs { system = "x86_64-linux"; };
      };
      defaults = { pkgs, ... }: { /* shared config */ };
      node-01 = {
        deployment.targetHost = "192.168.1.101";
        deployment.tags = [ "web" ];
        imports = [ ./hosts/node-01/configuration.nix ];
      };
    };
  };
}
```

#### Deployment Options Reference

| Option | Type | Default | Purpose |
|--------|------|---------|---------|
| `deployment.targetHost` | string or null | node attribute name | SSH hostname/IP |
| `deployment.targetUser` | string or null | `"root"` | SSH login user |
| `deployment.targetPort` | int or null | `null` (22) | SSH port |
| `deployment.tags` | list of string | `[]` | Tags for `--on` targeting |
| `deployment.buildOnTarget` | bool | `false` | Build on target instead of locally |
| `deployment.allowLocalDeployment` | bool | `false` | Allow deploying to the local machine |
| `deployment.replaceUnknownProfiles` | bool | `true` | Deploy even if target has unknown profile |
| `deployment.keys` | attrset | `{}` | Out-of-band secrets (never in Nix store) |

#### CLI Commands

```bash
# Build all configurations (no deploy)
colmena build

# Deploy all nodes (switch = activate immediately)
colmena apply

# Deploy with specific goal
colmena apply --goal switch       # activate now (default)
colmena apply --goal boot         # activate on next boot only
colmena apply --goal test         # activate now, revert on reboot
colmena apply --goal dry-activate # show what would change, no activation

# Target by tag
colmena apply --on @web
colmena apply --on '@infra-*'       # glob patterns supported

# Target by name
colmena apply --on node-01,node-02

# Build on target (avoids copying large closures)
colmena apply --build-on-target

# Limit parallelism (default: 10)
colmena apply --parallel 3

# Interactive eval/debug
colmena repl   # nix repl with nodes attrset available

# Run command on matching hosts
colmena exec --on @web -- systemctl status nginx

# Upload secrets only
colmena upload-keys

# Evaluate an expression against the hive
colmena eval -E '{ nodes, ... }: builtins.attrNames nodes'
```

#### `deployment.keys` — Out-of-Band Secrets

Colmena's key deployment feature transfers secrets to nodes without ever putting them in the Nix store (unlike sops-nix, which stores encrypted secrets in the store):

```nix
deployment.keys."github-token" = {
  text = "ghp_xxxx";          # or: keyFile = ./path/to/secret;
  destDir = "/etc/secrets";
  user = "myapp";
  group = "myapp";
  permissions = "0400";
};
```

Keys are uploaded via a separate SSH connection after the main activation, using a keyed transfer that doesn't involve the Nix store at all.

### 3.4 `nixos-anywhere`: Bootstrap NixOS from SSH

`nixos-anywhere` (`github.com/nix-community/nixos-anywhere`) installs NixOS onto any Linux machine reachable via SSH, without physical access. It is primarily used for:
- Initial provisioning of bare metal servers (Hetzner, Latitude.sh, etc.)
- Reinstalling a running Linux machine (cloud VM, existing Ubuntu server)

#### Prerequisites

**Source machine** (where you run the command):
- Nix installed

**Target machine**:
- x86_64-linux (ARM64 support exists but is less tested)
- kexec support enabled in kernel
- SSH accessible as root (or sudo-able user)
- ≥1 GB RAM (excluding swap)

#### How It Works: kexec Boot

When the target runs a non-NixOS OS:

1. nixos-anywhere uploads a NixOS kexec image via SSH
2. Executes `kexec` on the target to boot into the NixOS installer environment (without rebooting — the running kernel is replaced in-memory)
3. Inside the NixOS environment: runs disko to partition and format disks
4. Runs `nixos-install` with the specified configuration
5. Reboots into the new NixOS system

If the target is already a NixOS installer or rescue system, the kexec step is skipped.

#### Basic Command Syntax

```bash
# Run directly (no installation required)
nix run github:nix-community/nixos-anywhere -- \
  --flake .#my-host \
  root@192.168.1.100

# With hardware config generation
nix run github:nix-community/nixos-anywhere -- \
  --generate-hardware-config nixos-generate-config ./hosts/my-host/hardware-configuration.nix \
  --flake .#my-host \
  root@192.168.1.100

# With nixos-facter (more detailed hardware detection)
nix run github:nix-community/nixos-anywhere -- \
  --generate-hardware-config nixos-facter ./hosts/my-host/facter.json \
  --flake .#my-host \
  root@192.168.1.100
```

The `--flake .#my-host` argument refers to `nixosConfigurations.my-host` in your `flake.nix`.

#### Minimal NixOS Configuration with disko

```nix
# flake.nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    disko = { url = "github:nix-community/disko"; inputs.nixpkgs.follows = "nixpkgs"; };
  };
  outputs = { nixpkgs, disko, ... }: {
    nixosConfigurations.my-host = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        disko.nixosModules.disko  # enables disko integration
        ./hosts/my-host/disk-config.nix
        ./hosts/my-host/configuration.nix
      ];
    };
  };
}

# disk-config.nix
{
  disko.devices.disk.main = {
    device = "/dev/nvme0n1";
    type = "disk";
    content = {
      type = "gpt";
      partitions = {
        ESP = {
          size = "512M";
          type = "EF00";
          content = { type = "filesystem"; format = "vfat"; mountpoint = "/boot"; };
        };
        root = {
          size = "100%";
          content = {
            type = "filesystem";
            format = "ext4";
            mountpoint = "/";
          };
        };
      };
    };
  };
}
```

### 3.5 `disko`: Declarative Disk Partitioning

disko (`github.com/nix-community/disko`) is the declarative disk partitioning tool that fills the gap in NixOS's otherwise-declarative approach. Before disko, disk partitioning and formatting were manual steps during installation.

#### Device Type Hierarchy

disko supports recursive composition of storage technologies:

| Layer | Types |
|-------|-------|
| Physical disk | `disk` |
| Partition table | `gpt`, `msdos` |
| Partition | `partition` |
| Volume management | `lvm_vg` (with `lvm_lv`), `mdadm`, `zpool` |
| Encryption | `luks` |
| Filesystem | `filesystem` (`ext4`, `btrfs`, `xfs`, `vfat`, `tmpfs`, `swap`) |
| Btrfs specifics | `btrfs` with `subvolumes` |
| ZFS specifics | `zpool` with `datasets` |

#### ZFS Example (Relevant for forge-metal)

```nix
# disk-config.nix
{
  disko.devices = {
    disk.nvme = {
      device = "/dev/nvme0n1";
      type = "disk";
      content = {
        type = "gpt";
        partitions = {
          boot = { size = "1M"; type = "EF02"; };  # BIOS boot
          ESP  = { size = "512M"; type = "EF00"; content = { type = "filesystem"; format = "vfat"; mountpoint = "/boot"; }; };
          zfs  = { size = "100%"; content = { type = "zfs"; pool = "rpool"; }; };
        };
      };
    };

    zpool.rpool = {
      type = "zpool";
      options = {
        ashift = "12";
        autotrim = "on";
      };
      rootFsOptions = {
        compression = "lz4";
        "com.sun:auto-snapshot" = "false";
      };
      mountpoint = "/";
      datasets = {
        "local/root" = {
          type = "zfs_fs";
          options.mountpoint = "legacy";
          mountpoint = "/";
        };
        "local/nix" = {
          type = "zfs_fs";
          options.mountpoint = "legacy";
          mountpoint = "/nix";
        };
        "safe/home" = {
          type = "zfs_fs";
          options.mountpoint = "legacy";
          mountpoint = "/home";
        };
      };
    };
  };
}
```

#### Operating Modes

```bash
# Format only (destructive — wipes disks)
disko --mode format disk-config.nix

# Mount only (non-destructive — mounts existing partitions)
disko --mode mount disk-config.nix

# Both (typical usage during installation)
disko --mode destroy,format,mount disk-config.nix

# Via nixos-anywhere (automatic — disko is called internally)
nix run github:nix-community/nixos-anywhere -- --flake .#host root@ip
```

#### `diskoConfigurations` Flake Output

disko can also export configurations as a flake output (less common than inline `disko.devices`):

```nix
outputs = { disko, ... }: {
  diskoConfigurations.default = {
    disko.devices = { /* ... */ };
  };
};
```

#### NixOS Integration

When `disko.nixosModules.disko` is imported, disko generates `fileSystems.*` and `swapDevices` options from `disko.devices`, replacing manual `hardware-configuration.nix` filesystem entries. This means the disk layout is the single source of truth for both the installer and the running system.

### 3.6 `morph`: Older Fleet Tool

Morph (`github.com/DBCDK/morph`) is an older Haskell-based NixOS fleet deployment tool, a predecessor to colmena. Still used in some organizations.

```nix
# network.nix (morph style, not flake-native)
let
  pkgs = import <nixpkgs> {};
in {
  network = {
    description = "my-fleet";
    storage.legacy = {};
  };

  "node-01" = { config, pkgs, ... }: {
    deployment.targetHost = "192.168.1.101";
    imports = [ ./hosts/node-01.nix ];
  };
}
```

```bash
morph deploy ./network.nix switch  # activate immediately
morph deploy ./network.nix boot    # activate on next boot
morph check ./network.nix          # dry-run check
```

Morph lacks flake-native support and parallel deployment is limited. Colmena is the recommended modern replacement.

### 3.7 Tool Comparison: `nix copy` Baseline vs Fleet Tools

| Approach | Parallelism | Rollback | Multi-profile | Flake-native | Secrets | Use when |
|----------|-------------|----------|---------------|-------------|---------|---------|
| `nix copy` + `ssh` | Manual | Manual | No | Yes | Manual | 1-2 hosts, simple setup |
| `deploy-rs` | Parallel | Magic rollback | Yes | Yes | Via sops-nix | Multiple profiles per host, need rollback safety |
| `colmena` | Parallel (configurable) | Via NixOS generations | No (single system profile) | Yes | `deployment.keys` | Tag-based fleet targeting, operations |
| `morph` | Sequential/limited | Via NixOS generations | No | No | No | Legacy/migration |
| `nixos-anywhere` | N/A (bootstrap only) | N/A | N/A | Yes | N/A | Initial provisioning only |

#### The `nix copy` Bootstrap Pattern

For a single-node setup (forge-metal's current state), the full toolchain of deploy-rs or colmena is often overkill. The `nix copy` approach from [deployment-and-secrets.md](deployment-and-secrets.md) remains practical:

```bash
# Build + push + activate in three commands
nix build .#nixosConfigurations.my-host.config.system.build.toplevel
nix copy --to ssh://root@ip .#nixosConfigurations.my-host.config.system.build.toplevel
ssh root@ip '/nix/store/<hash>-nixos-system/bin/switch-to-configuration switch'
```

`nixos-rebuild switch --flake .#my-host --target-host root@ip` is the higher-level wrapper that does the same thing:

```bash
# Equivalent one-liner (requires nixos-rebuild available locally)
nixos-rebuild switch --flake .#my-host --target-host root@ip --use-remote-sudo
```

For forge-metal specifically: the current Ansible-based deployment (`make deploy`) is an alternative that's idiomatic for the existing codebase. Moving to deploy-rs or colmena is a future option when the node count grows beyond 2-3.

---

## Key Takeaways for forge-metal

### Language Patterns

- **`with pkgs;` in `flake.nix`**: The `forge-metal/flake.nix` uses `with pkgs;` in `shellHook`. This is acceptable because the scope is narrow (inside `mkShell`), not file-level. If new packages are added, use `inherit (pkgs) pkg1 pkg2;` instead of extending the `with` scope.

- **`rec {}` in flake.nix**: The `serverProfile = pkgs.buildEnv { ... }` definition does not need `rec` — it uses `let..in` style already. Avoid `rec` for the top-level flake outputs attrset.

- **`builtins.addErrorContext`**: Add to complex helper functions in `flake.nix` to make CI failures debuggable:
  ```nix
  serverProfile = builtins.addErrorContext "while building server-profile" (
    pkgs.buildEnv { ... }
  );
  ```

### Determinate Nix vs Upstream

- If forge-metal's CI uses `DeterminateSystems/nix-installer-action`, it uses Determinate Nix — flakes and lazy trees are enabled by default.
- Lazy trees' `src = ./.` warnings may appear. The fix is `src = builtins.path { path = ./.; name = "source"; }` — already the recommended pattern.

### Fleet Management

- **Current scale (1 node)**: `nixos-rebuild switch --flake .#my-host --target-host` or the Ansible-based `make deploy` is sufficient.
- **Future scale (2-10 nodes)**: colmena is the most natural fit — tag-based targeting maps well to forge-metal's "canary then fleet" deployment pattern described in the Makefile.
- **Initial provisioning**: nixos-anywhere + disko would replace the current Terraform + Ansible provisioning flow for the OS layer. Terraform would still manage the Latitude.sh API (server creation, SSH keys).
- **disko + ZFS**: disko's ZFS support (`zpool` device type with `datasets`) can declare the ZFS pool configuration that forge-metal currently sets up via Ansible roles.

---

## Sources

- Nix reference manual: `nix.dev/manual/nix/stable/language/`
- Nix language scope docs: `nix.dev/manual/nix/stable/language/scope.html`
- Nix string context docs: `nix.dev/manual/nix/stable/language/string-context.html`
- Nix advanced attributes: `nix.dev/manual/nix/stable/language/advanced-attributes.html`
- Nix best practices: `nix.dev/guides/best-practices`
- Nix anti-patterns: `nix.dev/anti-patterns/language`
- Determinate Nix: `determinate.systems/nix/`
- Determinate installer: `determinate.systems/posts/determinate-nix-installer`
- Lazy trees research: [advanced-topics.md](advanced-topics.md)
- deploy-rs README: `github.com/serokell/deploy-rs`
- colmena docs: `colmena.cli.rs/unstable/`
- colmena CLI reference: `colmena.cli.rs/unstable/reference/cli.html`
- colmena deployment reference: `colmena.cli.rs/unstable/reference/deployment.html`
- nixos-anywhere quickstart: `github.com/nix-community/nixos-anywhere/blob/main/docs/quickstart.md`
- disko README: `github.com/nix-community/disko`
- NixOS wiki on Lix: `wiki.nixos.org/wiki/Lix`
