# Flake Dependency Management: follows, Diamonds, and Tooling

## The `follows` Mechanism

Flake inputs form a DAG. Each input can have its own inputs, forming a tree of potentially duplicated versions. The `follows` directive short-circuits this by making an input's dependency point to an already-declared input:

```nix
inputs = {
  nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  home-manager = {
    url = "github:nix-community/home-manager";
    inputs.nixpkgs.follows = "nixpkgs";  # reuse our nixpkgs
  };
  hyprland = {
    url = "github:hyprwm/Hyprland";
    inputs.nixpkgs.follows = "nixpkgs";  # reuse our nixpkgs
  };
};
```

The `follows` value is a `/`-separated path of input names from the root flake. So `inputs.A.inputs.B.follows = "X"` means: "A's input named B should be the same node as our root input X."

Without `follows`, `home-manager`'s nixpkgs and your nixpkgs are pinned to different revisions — two separate `/nix/store/` paths for all of nixpkgs.

## The Diamond Dependency Problem

Consider:
```
Your flake
├── Input A → nixpkgs@rev1
└── Input B → nixpkgs@rev2
```

Even with the same nixpkgs URL, if A and B pin different revisions, you have two nixpkgs instances. Every package built by A uses a different store path than packages from B. This has two consequences:

1. **Binary cache misses**: cache.nixos.org pre-builds packages for specific nixpkgs revisions. Mixed revisions mean some packages won't have cache hits.
2. **ABI incompatibility**: if A provides a library and B provides an executable that links against it, and they used different nixpkgs revisions (different glibc), you get link failures or crashes at runtime. Issue [#5570](https://github.com/NixOS/nix/issues/5570) documents this as a recognized design-level problem.

The fix requires adding `follows` everywhere — recursively, down through all transitive inputs. This is the "ergonomics problem": if 5 of your inputs all bring in nixpkgs, you need 5 `follows` directives.

A Sourcegraph query found over 4,100 `flake-utils_*` entries in lock files, confirming that most projects do not properly reconcile transitive `flake-utils` instances. The same problem applies to `nixpkgs`.

Source: [nixcademy.com/posts/1000-instances-of-flake-utils](https://nixcademy.com/posts/1000-instances-of-flake-utils/)

## `legacyPackages` and Eval-Time Deduplication

A related benefit of `follows`: when two inputs share the same nixpkgs node (identical `rev` + `narHash`), their `legacyPackages.${system}` expressions are memoized by the Nix evaluator. Nix memoizes `import` at the function level, not the call level — but because the same file is being imported with the same arguments, the call sites are deduplicated through the thunk cache.

If two inputs use `import nixpkgs { inherit system; }` independently (without `follows`), Nix "returns two fresh thunks ... and re-evaluates them all over again." With `follows`, both resolve to the same attribute set value, evaluated once.

Source: [discourse.nixos.org](https://discourse.nixos.org/t/using-nixpkgs-legacypackages-system-vs-import/17462)

## `flake-utils`: What It Does and Why It's Controversial

`flake-utils.lib.eachDefaultSystem` (and `eachSystem`) wraps a per-system function and generates all the `packages.${system}`, `devShells.${system}`, etc. attributes in one call. Used in forge-metal's `flake.nix`:

```nix
flake-utils.lib.eachDefaultSystem (system:
  let pkgs = nixpkgs.legacyPackages.${system};
  in { devShells.default = ...; packages.default = ...; })
```

**Problems with `flake-utils`**:

1. **Doesn't validate output placement**: applying `eachDefaultSystem` to outputs that shouldn't have a `${system}` layer produces nonsense:
   - `nixosConfigurations` becomes `nixosConfigurations.x86_64-linux.myhost` — wrong schema
   - `overlays` becomes `overlays.x86_64-linux.default` — wrong schema
   Flake-utils silently generates the wrong structure with no error.

2. **Double system nesting**: manually mixing `eachDefaultSystem` and explicit system attributes produces `packages.x86_64-linux.x86_64-linux.default`.

3. **Lock file pollution**: `flake-utils` appears as a node in your `flake.lock`, and in all consumers' lock files. If upstream flakes also use `flake-utils` without a `follows` directive, you get multiple `flake-utils` instances (the "1000 instances" problem).

4. **Doesn't scale to `nixosConfigurations`**: system-independent outputs like `nixosConfigurations`, `overlays`, and `nixosModules` must be defined outside `eachDefaultSystem` anyway.

Source: [ayats.org/blog/no-flake-utils](https://ayats.org/blog/no-flake-utils)

**The minimal alternative** (zero external deps):

```nix
let
  forAllSystems = f:
    nixpkgs.lib.genAttrs ["x86_64-linux" "aarch64-linux" "aarch64-darwin" "x86_64-darwin"]
      (system: f nixpkgs.legacyPackages.${system});
in {
  packages = forAllSystems (pkgs: { default = pkgs.hello; });
  devShells = forAllSystems (pkgs: { default = pkgs.mkShell { buildInputs = [pkgs.go]; }; });
}
```

## `flake-parts`: Module System for Flakes

`flake-parts` (github.com/hercules-ci/flake-parts) uses the NixOS module system to build flake outputs. It provides:

- `perSystem`: a module-level option equivalent to `eachDefaultSystem`, but type-checked
- Module system validation: `nixosConfigurations` placed inside `perSystem` is rejected with a type error (it shouldn't have a system prefix)
- Composability: split `flake.nix` into multiple focused modules in separate files
- Ecosystem: many projects publish flake-parts modules (devenv, treefmt, mission-control, etc.)

The key advantage over `flake-utils`: **type checking via the module system prevents the malformed outputs that `flake-utils` silently generates**.

`flake-parts` does not add `flake-utils` to the dependency graph.

Source: [flake.parts](https://flake.parts/)

## `legacyPackages`: Why "Legacy"?

The name is historical. nixpkgs predates flakes and cannot fit its recursive, lazy attribute set structure into the flat `packages.${system}.${name}` schema that `nix flake show` expects to enumerate. If nixpkgs used `packages`, `nix flake show nixpkgs` would try to evaluate all ~80,000 packages to build the tree — unusably slow.

When Nix sees `legacyPackages`, it displays "(omitted)" instead of recursing into the attribute set. This keeps `nix flake show` fast at the cost of less information.

`nixpkgs.legacyPackages.${system}` is defined in nixpkgs's `flake.nix` as:
```nix
legacyPackages = forAllSystems (system: import ./. { inherit system; });
```

So `nixpkgs.legacyPackages.x86_64-linux` is equivalent to `import nixpkgs { system = "x86_64-linux"; }` — with the advantage that when shared via `follows`, the evaluated result is memoized rather than re-evaluated per consumer.

Output lookup priority: `nix build .#foo` searches `packages.${system}.foo` first, then falls back to `legacyPackages.${system}.foo`.

## `inputsFrom` in `mkShell`

`pkgs.mkShell { inputsFrom = [someDerivation]; }` extracts and merges the following from `someDerivation`:
- `buildInputs`, `nativeBuildInputs`
- `propagatedBuildInputs`, `propagatedNativeBuildInputs`
- `shellHook` (concatenated in order)

This is more than just "getting packages" — it pulls the entire build dependency graph of the derivation. The common pattern:

```nix
devShells.default = pkgs.mkShell {
  inputsFrom = builtins.attrValues self.packages.${system};  # inherit all build deps
  buildInputs = [ pkgs.extra-dev-tool ];  # add dev-only tools on top
};
```

This ensures the dev shell always has the same compiler, libraries, and environment as the actual build — without manually maintaining a duplicated list.

The `shellHook` concatenation means if `someDerivation` has a `shellHook` that sets up environment variables, those are automatically included in your dev shell.

## Alternatives to Flakes for Dependency Pinning

For projects that want pinning without flakes:

- **npins** (github.com/andir/npins) — pin sources to specific hashes, generates Nix expressions, imports from Niv lock files or `flake.lock`
- **niv** (github.com/nmattia/niv) — the original pins-via-JSON approach; JSON-based source pinning
- **dream2nix** — framework for converting language-ecosystem lock files (package.json, Cargo.lock) to Nix; handles the "package set" problem that flakes don't

For the "use flake for entrypoint but avoid flake for composition" pattern: `flake-compat` (github.com/NixOS/flake-compat) reads `flake.lock` and calls the `outputs` function from a traditional `default.nix`, enabling `nix-shell` / `nix-build` workflows against a flake without `--experimental-features`.

## The Cross-Compilation Problem

Flakes cannot support cross-compilation cleanly because `packages.${system}` has only one system dimension. Cross-compilation needs both `localSystem` (build host) and `crossSystem` (target). The traditional nixpkgs approach:

```nix
pkgs = import nixpkgs {
  localSystem = "x86_64-linux";
  crossSystem = "aarch64-linux";
};
```

In a flake, you'd need a separate output like `packages.x86_64-linux-cross-aarch64.default` — an ad-hoc naming convention not standardized in the flake schema.

The jade.fyi article "flakes aren't real" argues this is a fundamental architectural flaw: `callPackage` resolves cross-compilation dependencies automatically through nixpkgs internals, but flakes expose packages as pre-evaluated attribute sets that lose this resolution context.

Workaround: expose cross-compiled packages via `legacyPackages` (which can be an arbitrary attribute set) rather than `packages`.
