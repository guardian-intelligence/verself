# Nix Flakes Source Code Deep Dive

## `call-flake.nix`: How Self Is Actually Built

The heart of flake evaluation is `src/libflake/call-flake.nix` in the Nix source tree. This is the Nix expression that constructs the `self` reference and wires up all inputs. Here is the complete current source:

```nix
# This is a helper to callFlake() to lazily fetch flake inputs.
lockFileStr:   # JSON string of flake.lock
overrides:     # map of node ID → { sourceInfo, subdir }
fetchTreeFinal: # prim_fetchFinalTree

let
  inherit (builtins) mapAttrs;
  lockFile = builtins.fromJSON lockFileStr;

  resolveInput =
    inputSpec:
    if builtins.isList inputSpec
    then getInputByPath lockFile.root inputSpec
    else inputSpec;

  getInputByPath = nodeName: path:
    if path == []
    then nodeName
    else getInputByPath
      (resolveInput lockFile.nodes.${nodeName}.inputs.${builtins.head path})
      (builtins.tail path);

  allNodes = mapAttrs (key: node:
    let
      hasOverride  = overrides ? ${key};
      isRelative   = node.locked.type or null == "path"
                     && builtins.substring 0 1 node.locked.path != "/";
      parentNode   = allNodes.${getInputByPath lockFile.root node.parent};

      sourceInfo =
        if hasOverride         then overrides.${key}.sourceInfo
        else if isRelative     then parentNode.sourceInfo
        else fetchTreeFinal (node.info or {} // removeAttrs node.locked ["dir"]);

      subdir  = overrides.${key}.dir or node.locked.dir or "";
      outPath =
        if !hasOverride && isRelative
        then parentNode.outPath + (if node.locked.path == "" then "" else "/" + node.locked.path)
        else sourceInfo.outPath + (if subdir == "" then "" else "/" + subdir);

      flake   = import (outPath + "/flake.nix");
      inputs  = mapAttrs (inputName: inputSpec:
        allNodes.${resolveInput inputSpec}.result
      ) (node.inputs or {});

      outputs = flake.outputs (inputs // { self = result; });

      result =
        outputs
        // sourceInfo   # adds lastModified, narHash, outPath, etc.
        // {
          inherit outPath;     # shadows sourceInfo.outPath with flake subdir path
          inherit inputs;
          inherit outputs;
          inherit sourceInfo;
          _type = "flake";
        };
    in {
      result =
        if node.flake or true
        then assert builtins.isFunction flake.outputs; result
        else sourceInfo // { inherit sourceInfo outPath; };
      inherit outPath sourceInfo;
    }
  ) lockFile.nodes;

in allNodes.${lockFile.root}.result
```

Source: [github.com/NixOS/nix/blob/master/src/libflake/call-flake.nix](https://github.com/NixOS/nix/blob/master/src/libflake/call-flake.nix)

### Key observations from the source

**1. `self` is `outputs // sourceInfo // { outPath, inputs, outputs, sourceInfo, _type }`**

The merge order matters. `outputs` is applied first (so package definitions etc. are in there), then `sourceInfo` is overlaid (bringing `narHash`, `lastModified`, etc.), then the final `//` shadows `sourceInfo.outPath` with the subdir-corrected `outPath`. This is how `self.outPath` can differ from `self.sourceInfo.outPath` when `?dir=` is used.

**2. The `follows` mechanism is a list-path traversal**

An `inputSpec` in `flake.lock` is either a string (node name) or a **list of strings** (a follows path). The `resolveInput` function handles both: if it's a list, it walks `getInputByPath` starting from the root. So `inputs.A.inputs.B.follows = "X/Y"` becomes the list `["X", "Y"]` in the lock file, resolved by walking `root→X→Y`.

**3. `flake = false` inputs skip `flake.outputs` entirely**

The conditional `if node.flake or true` means: if the node has `flake: false` in its lock entry, the result is just `sourceInfo // { inherit sourceInfo outPath; }` — no `outputs`, no `inputs`, just the source tree with its metadata. The `or true` default means unspecified `flake` field is treated as a real flake.

**4. Relative path inputs reference `parentNode`**

There is a `parent` field in lock file nodes that isn't documented in the official manual. Relative path inputs (type=`path`, path not starting with `/`) inherit their `sourceInfo` from the parent node — meaning they share the same fetched tree as their containing flake. The `outPath` is constructed by appending the relative path to the parent's `outPath`.

**5. `overrides` enable `--override-input`**

The `overrides` parameter is populated when you pass `--override-input foo path:/local/dir`. It bypasses `fetchTreeFinal` for that node, substituting a local source tree. This is how `nix develop --override-input nixpkgs ./my-nixpkgs` works.

**6. `outputs` is set on `self` separately from the merged result**

Note that `result` includes both `outputs` (the raw attribute set from `flake.outputs`) and the merged top-level attributes from that same `outputs`. So `self.packages` (via the merge) and `self.outputs.packages` (via the explicit `inherit outputs`) are the same object. This means:

```nix
self.packages.x86_64-linux == self.outputs.packages.x86_64-linux  # true
```

## The Lazy Evaluation Self-Reference Trap

The `self = result` binding in `call-flake.nix` creates a circular definition that works only because of Nix's lazy evaluation. The critical issue is **where** you access `self` within the outputs function:

```nix
# WRONG — accesses self in the function body (strict position):
outputs = { self, ... }:
  builtins.trace "${self.outPath}" {   # forces self before result is defined
    packages = {};
  };

# CORRECT — accesses self in an attribute value (lazy position):
outputs = { self, ... }: {
  packages.x86_64-linux.default =
    pkgs.callPackage ./pkg.nix { inherit self; };   # lazy: evaluated on demand
};
```

The function body `builtins.trace "${self.outPath}" { ... }` evaluates `self.outPath` immediately when the outputs function is called — before `result` is fully assembled. The attribute value form is lazy: `self.outPath` is only evaluated when `packages.x86_64-linux.default` is accessed, by which time `result` is complete.

Source: [github.com/NixOS/nix/issues/8300](https://github.com/NixOS/nix/issues/8300)

This is why `system.configurationRevision = self.rev or "dirty"` works safely — `nixosConfigurations` is evaluated lazily when `nixos-rebuild` activates, not at outputs-function call time.

## `builtins.fetchTree` vs `builtins.fetchGit`

`fetchTree` is the internal primitive that backs all flake input fetching. It is a generalization of `fetchGit`, `fetchMercurial`, and `fetchTarball` — implemented as a single C++ function calling the fetcher API.

A non-obvious coupling: `builtins.fetchTree` requires the `flakes` experimental feature to be enabled, even though it is conceptually independent. Issue [#5541](https://github.com/NixOS/nix/issues/5541) documents this as an artificial constraint: "`fetchTree` was architecturally coupled to flakes during implementation." The Nix team labeled this "idea approved" for eventual decoupling, but it remains tied to the `flakes` feature flag.

Practical implication: you cannot use `builtins.fetchTree` in traditional `default.nix` files without also enabling the `flakes` feature — even if you have no `flake.nix`.

## `nix flake archive`: Moving Entire Dependency Graphs

`nix flake archive` copies a flake **and all its inputs** (the entire source tree graph, not just build outputs) to a Nix store. This is fundamentally different from `nix copy`:

| Command | What it copies | Use case |
|---------|---------------|----------|
| `nix copy` | Build output closures (derivation results) | Deploy built artifacts |
| `nix flake archive` | Flake source trees (all inputs from `flake.lock`) | Airgapped evaluation |

```bash
# Copy all flake inputs to a local binary cache:
nix flake archive --to file:///tmp/flake-cache

# Copy to a remote store (inputs only, not build outputs):
nix flake archive --to ssh://remote-host

# Inspect what would be transferred (dry run with JSON):
nix flake archive --dry-run --json | jq .
```

The key use case is **airgapped/offline CI**: archive all inputs before entering a network-isolated environment, so `nix build` can evaluate the flake without internet access. The archived inputs go into the store; subsequent `nix build` commands find them locally.

Source: [nix.dev/manual/nix/2.28/.../nix3-flake-archive](https://nix.dev/manual/nix/2.28/command-ref/new-cli/nix3-flake-archive)

## `nix flake metadata`: Inspecting Lock State

```bash
nix flake metadata .
# Outputs: Resolved URL, Locked URL, Description, Path, Revision, Last modified
# and a tree of all inputs with their lock state

nix flake metadata --json . | jq .
# JSON fields: description, lastModified, locked (narHash, rev, owner, repo, type), resolvedUrl, lockedUrl
```

The `nix flake metadata` output exposes exactly what is in `flake.lock` in human-readable form — useful for auditing the full dependency tree. Issue [#9303](https://github.com/NixOS/nix/issues/9303) documents a bug where the locked URL output by `metadata` (which includes the `narHash`) cannot be used back as input to Nix commands — it crashes because the hash-in-URL form isn't accepted as a valid flake reference.

## `builtins.getFlake`: The Backdoor to Flakes Without Files

`builtins.getFlake "nixpkgs"` evaluates a flake reference without a `flake.nix` in the current directory. It returns the complete flake object (same structure as what `call-flake.nix` produces). This enables:

```nix
# In a traditional default.nix or REPL:
let nixpkgs = builtins.getFlake "nixpkgs"; in
nixpkgs.legacyPackages.x86_64-linux.hello
```

Surprises:
- `builtins.getFlake` also requires the `flakes` experimental feature
- It uses the registry to resolve indirect references
- In pure eval mode, `builtins.getFlake` is available but the flake reference must be locked (have a specific rev or narHash)

## `nix-direnv`: Production Eval Cache Workaround

The official eval cache has the "path: flakes bypass cache" problem. `nix-direnv` solves this for development workflows:

1. Calls `nix print-dev-env` to capture the shell environment as JSON
2. Caches the result in `.direnv/` keyed by `flake.nix` + `flake.lock` content hash
3. Creates a GC root pointing at the cached environment derivation
4. Only re-evaluates when `flake.nix`, `flake.lock`, `shell.nix`, or `default.nix` change

The GC root is critical: without it, `nix-collect-garbage` removes the cached shell derivation, forcing re-evaluation. `nix develop` without `nix-direnv` creates no GC root and the evaluated environment is garbage-collected after the session ends.

`nix-direnv` also keeps the **previous working devShell** if a new version fails to evaluate — graceful degradation that `nix develop` doesn't provide.

Source: [github.com/nix-community/nix-direnv](https://github.com/nix-community/nix-direnv)
