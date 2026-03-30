# Nix Store Path Computation, NIX_PATH/Channels, and `builtins.fetchGit`

Three interlocking topics that sit at the foundation of how Nix identifies content, resolves legacy imports, and fetches Git sources. All findings traced to primary sources: Nix source (`github.com/NixOS/nix`), official manual, and Nix Pills.

---

## Part 1: How Store Paths Are Actually Computed

Every path in `/nix/store` — whether a source file, a `.drv`, or a build output — has a deterministic 32-character base-32 hash prefix. Almost no documentation explains the exact formula. This section does.

### The Store Path Structure

```
/nix/store/<digest>-<name>
           ^^^^^^^^
           32 chars of Nix base-32 encoding of 160-bit truncated SHA-256
```

A Nix "base-32" character set is `0123456789abcdfghijklmnpqrsvwxyz` (standard base-32 but with some characters removed to avoid ambiguity). The encoding processes bytes from the end of the hash (reverse of base-16).

### The Universal Formula

All store paths share the same outer computation:

```
fingerprint = type ":" "sha256:" inner-digest ":" store-dir ":" name
store-path  = store-dir "/" base32(sha256(fingerprint)[0..20]) "-" name
```

Where:
- `type` is one of `"text"`, `"source"`, or `"output:out"` (or `"output:<id>"` for other outputs)
- `inner-digest` is the lowercase base-16 SHA-256 of the **inner fingerprint** (see per-type below)
- `store-dir` is `/nix/store` (or the configured store directory)
- `name` is the derivation name (e.g., `hello-2.12`)
- The outer SHA-256 is truncated to the first 160 bits (20 bytes), then base-32 encoded

Source: Nix Pills chapter 18, `doc/manual/source/protocols/store-path.md`.

---

### Type 1: Source Paths (from `builtins.path`, path literals, `nix-store --add`)

**Inner fingerprint:** The NAR serialization of the file system object.

**Step-by-step (from Nix Pills):**

```bash
# 1. Hash the NAR serialization of the file
nix-hash --type sha256 myfile
# → 2bfef67de873c54551d884fdab3055d84d573e654efa79db3c0d7b98883f9ee3

# Equivalent:
nix-store --dump myfile | sha256sum

# 2. Build the fingerprint string
echo -n "source:sha256:2bfef67de873c54551d884fdab3055d84d573e654efa79db3c0d7b98883f9ee3:/nix/store:myfile" \
  > myfile.str

# 3. Compute final digest
nix-hash --type sha256 --truncate --base32 --flat myfile.str
# → xv2iccirbrvklck36f1g7vldn5v58vck

# Result: /nix/store/xv2iccirbrvklck36f1g7vldn5v58vck-myfile
```

**Key property:** The `source:` prefix means the path hash is content-addressed against the NAR of the tree. This is why renaming or touching a file changes the store path of that source and all downstream derivations — the NAR changes, the inner digest changes, the fingerprint changes, the final hash changes.

**References with source type:** If the source file contains references to other store paths, those paths are appended to the fingerprint:

```
"source:sha256:" + inner-digest + ":/nix/store:name" + ":/nix/store/dep1" + ":/nix/store/dep2"
```

This is rare for plain sources but relevant for text files with embedded store paths.

---

### Type 2: Text Paths (`.drv` files, `builtins.toFile`)

**Inner fingerprint:** The literal string content written to the store path.

```
"text:" + ":".join(sorted(references)) + ":sha256:" + sha256(content) + ":/nix/store:" + name
```

Text paths are used for:
- Derivation (`.drv`) files themselves stored in the Nix store
- `builtins.toFile` outputs
- The output of `builtins.toPath` when writing a literal string

The `text:` type is distinct from `source:` to avoid collision: a `.drv` file and a source file with identical byte content must have different store paths because they have different semantics.

---

### Type 3: Input-Addressed Derivation Output Paths

When you evaluate `pkgs.hello`, Nix computes the output path of the derivation **before building anything**. The key insight: the output path depends only on all inputs, not on what the build actually produces.

**Inner fingerprint:** The ATerm serialization of the derivation **with all output paths replaced by empty strings** ("modulo fixed outputs").

**Step-by-step (from Nix Pills):**

```bash
# 1. Copy the .drv and blank out the output path
cp /nix/store/y4h73bmrc9ii5bxg6i7ck6hsf5gqv8ck-foo.drv myout.drv
sed -i 's,/nix/store/hs0yi5n5nw6micqhy8l1igkbhqdkzqa1-foo,,g' myout.drv
# The .drv now has an empty string where the output store path was

# 2. Hash the modified .drv
sha256sum myout.drv
# → 1bdc41b9649a0d59f270a92d69ce6b5af0bc82b46cb9d9441ebc6620665f40b5

# 3. Build fingerprint
echo -n "output:out:sha256:1bdc41b9649a0d59f270a92d69ce6b5af0bc82b46cb9d9441ebc6620665f40b5:/nix/store:foo" \
  > myout.str

# 4. Compute final digest
nix-hash --type sha256 --truncate --base32 --flat myout.str
# → hs0yi5n5nw6micqhy8l1igkbhqdkzqa1

# Result: /nix/store/hs0yi5n5nw6micqhy8l1igkbhqdkzqa1-foo
```

This circularity is resolved during instantiation: Nix solves the system by computing the output path, writing it into the `.drv`, then using the `.drv` (with the output path blanked) as the basis for the hash. It is a fixed-point computation.

**Multiple outputs:** For a derivation with `outputs = ["out" "dev" "lib"]`, each output gets its own fingerprint with its own type string: `"output:out"`, `"output:dev"`, `"output:lib"`. The inner fingerprint hash is computed from the same `.drv` (with all output paths blanked), but the type prefix differs.

**Why changes propagate:** If you change one line in a source file, the source store path changes. That changes the `src` environment variable in the downstream `.drv`. That changes the `.drv` file content. That changes the inner hash. That changes the output store path. Every dependent derivation gets a new store path too — even if the actual build output would be identical. This is why `lib.cleanSource` and `lib.fileset` exist: to filter out `.git/`, `node_modules/`, etc. from the source NAR, preventing irrelevant file changes from invalidating the entire cache.

---

### Type 4: Fixed-Output Derivation Paths

A FOD's output path is computed from the **declared output hash**, not the derivation inputs. This is a two-stage computation:

**Stage 1 — Encode the content hash:**
```
inner-fixed = "fixed:out:" + rec + algo + ":" + hash + ":"
```

Where `rec` is:
- `""` (empty) for `outputHashMode = "flat"` (single file hash)
- `"r:"` for `outputHashMode = "recursive"` or `"nar"` (NAR hash of directory)
- `"git:"` for `outputHashMode = "git"` (experimental)

**Stage 2 — Use as inner fingerprint for `"output:out"` type:**
```bash
# Example: outputHashMode = "flat", sha256 = "f3f3c4..."
echo -n "fixed:out:sha256:f3f3c4763037e059b4d834eaf68595bbc02ba19f6d2a500dce06d124e2cd99bb:" \
  > mycontent.str
sha256sum mycontent.str
# → 423e6fdef56d53251c5939359c375bf21ea07aaa8d89ca5798fb374dbcfd7639

echo -n "output:out:sha256:423e6fdef56d53251c5939359c375bf21ea07aaa8d89ca5798fb374dbcfd7639:/nix/store:bar" \
  > myfile.str
nix-hash --type sha256 --truncate --base32 --flat myfile.str
# → a00d5f71k0vp5a6klkls0mvr1f7sx6ch

# Result: /nix/store/a00d5f71k0vp5a6klkls0mvr1f7sx6ch-bar
```

**Consequence:** Two FODs with the same declared output hash and name produce the same store path regardless of what their builders do. The Nix daemon substitutes a FOD if a store path with the matching hash already exists — it does not re-run the builder. This is why FODs are the only derivations allowed network access (the content hash provides the tamper-proof guarantee).

---

### Content-Addressed (CA) Derivation Output Paths

CA derivations (behind the `ca-derivations` experimental feature) compute their output path **after building**, not before. The output path is based on the actual content of what was built.

There are two CA modes:
- `"ca:fixed:"` — same formula as FOD (declared hash, not from build)
- `"ca:floating:"` — truly content-addressed: the output hash is computed after build, then used to derive the store path

**Early cutoff:** With CA derivations, if two builds produce identical content (same NAR hash), they map to the same store path. This means an intermediate dependency can be rebuilt without triggering downstream rebuilds — as long as the output content is identical. This is sometimes called "input-addressed + early cutoff" and is the theoretical basis for better incremental builds.

---

### `builtins.hashString` and `builtins.hashFile`

```nix
builtins.hashString "sha256" "hello"
# → "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
# Output: lowercase base-16 hex

builtins.hashFile "sha256" ./myfile
# → base-16 hex of sha256 of file content (flat, not NAR)
```

Supported algorithms: `"md5"`, `"sha1"`, `"sha256"`, `"sha512"`.

Note that `builtins.hashFile` hashes the file content directly (flat mode), not as a NAR serialization. This differs from `nix-hash --type sha256 <file>` which hashes the NAR unless `--flat` is passed.

**The `lib.fakeHash` trick for FODs:**

When writing a new FOD (e.g., `buildGoModule` with `vendorHash`, or `fetchurl`), use an intentionally wrong hash to get the real hash from the error message:

```nix
# Use lib.fakeHash (or lib.fakeSha256 for legacy):
vendorHash = lib.fakeHash;
# lib.fakeHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="  (SRI format)
# lib.fakeSha256 = "0000000000000000000000000000000000000000000000000000"  (nix32 format)
```

When Nix attempts to build the FOD, it gets the actual hash from the network fetch and fails with:
```
error: hash mismatch in fixed-output derivation:
  specified: sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
       got: sha256-3XYHZANT6AFBV0BqegkAZHbba6oeDkIUCDwbATLMhAY=
```

Copy the `got:` value into your Nix expression. `lib.fakeHash` uses SRI format (`sha256-<base64>`); `lib.fakeSha256` uses the Nix32 format — use whichever matches the parameter you're setting.

---

### "Pointer Equality" in Nix

The most powerful property of this design: **same store path = same content, always**. The Nix store is a content-addressed write-once database. If `/nix/store/abc-hello-2.12` exists, it is guaranteed to contain exactly what was built from the inputs that hash to `abc`. There is no versioning, no mutation, no tagging. Two packages built from identical inputs on different machines on different days will have the same store path and can substitute for each other on any binary cache.

The `--check` flag tests this: `nix build --check` rebuilds a derivation and compares the output hash against the already-stored output. If they differ, the build is non-deterministic and Nix reports the discrepancy. The [R13Y project](https://r13y.com/) tracks reproducibility across nixpkgs.

---

## Part 2: `NIX_PATH`, Angle Brackets, and the Legacy→Flake Transition

### What `<nixpkgs>` Actually Does

The angle-bracket syntax `<nixpkgs>` is a **lookup path expression**. It desugars directly to:

```nix
builtins.findFile builtins.nixPath "nixpkgs"
```

`builtins.nixPath` is the parsed version of the `nix-path` configuration setting plus the `NIX_PATH` environment variable. It is a list of attribute sets:

```nix
# builtins.nixPath might look like:
[
  { prefix = "nixpkgs"; path = "/nix/var/nix/profiles/per-user/root/channels/nixpkgs"; }
  { prefix = "";         path = "/home/user/.nix-defexpr/channels"; }
]
```

`builtins.findFile` iterates through this list. For each entry, it checks if the lookup name matches the entry's `prefix`. If it matches (or the prefix is empty), it resolves the remainder of the path within the entry's `path` directory.

**Example resolution of `<nixpkgs/nixos>`:**
1. Check `{ prefix = "nixpkgs"; path = "/nix/var/.../nixpkgs" }` — matches prefix `nixpkgs`
2. Append remaining path `/nixos` → check for `/nix/var/.../nixpkgs/nixos`
3. If it exists, return that path

---

### `NIX_PATH` Format

```
NIX_PATH="nixpkgs=/nix/var/nix/profiles/per-user/root/channels/nixpkgs:nixos-config=/etc/nixos/configuration.nix:/home/user/dev"
```

Colon-separated entries. Each entry is either:
- `name=path` — named entry (sets `prefix = "name"`)
- `path` — unnamed entry (sets `prefix = ""`, searches entire directory for matches)

Priority (highest to lowest):
1. `--nix-path` / `-I` command-line flag
2. `NIX_PATH` environment variable
3. `nix-path` setting in `nix.conf`
4. Built-in default (includes `$HOME/.nix-defexpr/channels`, root's nixpkgs channel)

---

### `NIX_PATH` in Flake Context: The Core Gotcha

Pure flake evaluation sets `NIX_PATH=""` (empty). Any file evaluated in a flake that contains `<nixpkgs>` will throw at evaluation time:

```
error: file 'nixpkgs' was not found in the Nix search path (add it using $NIX_PATH or -I)
```

This happens even if `NIX_PATH` is set in your shell — flake evaluation ignores it when `--pure-eval` (the default) is active.

**Solutions:**

1. **Pass `--impure`** (not recommended for reproducibility):
   ```bash
   nix build --impure .#foo
   ```
   This re-enables `NIX_PATH` lookup but breaks hermetic evaluation.

2. **Use flake inputs instead** (correct approach):
   ```nix
   # In flake.nix, replace:
   import <nixpkgs> {}
   # With:
   inputs.nixpkgs.legacyPackages.${system}
   # Or:
   import inputs.nixpkgs { inherit system; }
   ```

3. **The `nix.nixPath` NixOS option** — for system-level impure contexts:
   ```nix
   nix.nixPath = [ "nixpkgs=${inputs.nixpkgs}" ];
   ```
   This sets `NIX_PATH` for processes running on the NixOS system, so `import <nixpkgs>` works in scripts and REPLs that run outside the flake evaluator. Does **not** affect flake pure evaluation.

---

### How Channels Work: The Full Picture

Channels are the pre-flake mechanism for getting access to nixpkgs. Despite the flakes migration, channels are still installed by default by the Nix installer.

**The `~/.nix-channels` file:**
```
https://nixos.org/channels/nixpkgs-unstable nixpkgs
https://nixos.org/channels/nixos-24.11 nixos-24.11
```
One entry per line: `<url> <name>`.

**What `nix-channel --update` does:**
1. Downloads `<url>/nixexprs.tar.gz` for each subscribed channel
2. Unpacks it into a new generation of the channels profile
3. The channels profile lives at:
   - Regular users: `$XDG_STATE_HOME/nix/profiles/channels` (or `~/.local/state/nix/profiles/channels`)
   - Root: `/nix/var/nix/profiles/per-user/root/channels`
4. The channel profile is just a regular Nix profile — `nix-channel --rollback` calls `nix-env --rollback` on it
5. The channel profile generation links to a `manifest.nix` listing all channel expressions

**How channels set NIX_PATH:**

The Nix installer adds to the shell profile (`~/.profile`, `~/.bashrc`, etc.):
```bash
export NIX_PATH="$HOME/.nix-defexpr/channels:nixpkgs=$HOME/.nix-defexpr/channels/nixpkgs"
```

`$HOME/.nix-defexpr/channels` is itself a symlink to `~/.local/state/nix/profiles/channels`. So `<nixpkgs>` resolves through: `NIX_PATH` → channel profile symlink → unpacked `nixexprs.tar.gz`.

**Channel subcommands:**

| Command | Effect |
|---------|--------|
| `nix-channel --add <url> [name]` | Adds entry to `~/.nix-channels`; does **not** fetch |
| `nix-channel --remove <name>` | Removes entry; does not delete unpacked channel |
| `nix-channel --update [names...]` | Downloads + creates new channel profile generation |
| `nix-channel --list` | Prints `~/.nix-channels` contents |
| `nix-channel --list-generations` | Equivalent to `nix-env --profile <channels-profile> --list-generations` |
| `nix-channel --rollback [gen]` | Reverts to previous (or specified) generation |

**Default channels for NixOS:**

| Channel URL | Purpose |
|-------------|---------|
| `https://nixos.org/channels/nixos-24.11` | Stable NixOS release |
| `https://nixos.org/channels/nixos-unstable` | NixOS unstable (includes NixOS tests) |
| `https://nixos.org/channels/nixpkgs-unstable` | nixpkgs unstable (no NixOS tests); faster updates |

`nixos-unstable` and `nixpkgs-unstable` track the same nixpkgs repository but `nixos-unstable` only advances when the full NixOS test suite passes on Hydra. `nixpkgs-unstable` advances more frequently (passes a smaller set of checks).

---

### `nix.registry` vs `nix.nixPath`

These are often confused — they serve different resolvers:

| Setting | Affects | Used by |
|---------|---------|---------|
| `nix.nixPath` / `NIX_PATH` | Lookup path resolution (`<name>` syntax) | `import <nixpkgs>`, `nix-build '<nixpkgs>' -A` |
| `nix.registry` | Flake registry resolution (indirect flake refs) | `nix build nixpkgs#hello`, `nix flake show nixpkgs` |

```nix
# NixOS flake — wire up both so both worlds work:
nix.nixPath = [ "nixpkgs=${inputs.nixpkgs}" ];

nix.registry = {
  nixpkgs.flake = inputs.nixpkgs;
  # This makes `nix run nixpkgs#hello` use the pinned nixpkgs from flake.lock
};
```

Without `nix.registry.nixpkgs`, `nix run nixpkgs#hello` resolves `nixpkgs` through the global flake registry at `channels.nixos.org/flake-registry.json`, which may point to a different revision than the one in your `flake.lock`.

---

### `nix-env` Legacy Commands vs Flake Equivalents

`nix-env` manages a user profile (default: `~/.nix-profile`) using the old `manifest.nix` format. The new `nix profile` command uses `manifest.json` (version 1) and is incompatible with `nix-env` on the same profile.

**Command mapping:**

| Legacy `nix-env` | Modern `nix profile` / `nix` | Notes |
|------------------|------------------------------|-------|
| `nix-env -i <name>` | `nix profile install nixpkgs#<name>` | `-i` matches by name; `#` uses attribute path |
| `nix-env -iA nixpkgs.<pkg>` | `nix profile install nixpkgs#<pkg>` | `-A` = attribute path |
| `nix-env -f '<nixpkgs>' -iA <pkg>` | `nix profile install nixpkgs#<pkg>` | `-f` selects the Nix expression file |
| `nix-env -e <name>` | `nix profile remove <name>` | Remove by derivation name |
| `nix-env -u` | `nix profile upgrade --all` | Upgrade all; `nix profile` only upgrades unlocked refs |
| `nix-env -u <name>` | `nix profile upgrade <name>` | Upgrade single package |
| `nix-env -q` | `nix profile list` | List installed packages |
| `nix-env -qa` | `nix search nixpkgs <term>` | Query available packages |
| `nix-build '<nixpkgs>' -A <pkg>` | `nix build nixpkgs#<pkg>` | Build without installing |
| `nix-shell -p <pkg>` | `nix shell nixpkgs#<pkg>` | Temporary shell with package |
| `nix-env --list-generations` | `nix profile history` | View profile generations |
| `nix-env --rollback` | `nix profile rollback` | Revert to previous generation |

**`nix-env --query` flags:**
- `-i` (default): query installed packages in current profile generation
- `-a`: query available packages in active Nix expression
- `-s` / `--status`: print `IPS` status chars (Installed/Present-on-system/Substitute-available)
- `-c` / `--compare-versions`: compare installed vs available versions (`<`/`=`/`>`)
- `--json`: machine-readable output
- `-A` / `--attr-path`: print the attribute path alongside names

**`nix-env --upgrade` flags:**
- `--lt` (default): only upgrade to strictly newer versions
- `--leq`: upgrade to same or newer (useful for reinstalling)
- `--eq`: upgrade to same version only (rebuilds against newer deps)
- `--always`: upgrade, downgrade, or lateral move — whatever is available

---

### Profile Manifest Formats: `manifest.nix` vs `manifest.json`

The two profile formats are **incompatible**. Mixing them on the same profile corrupts it:

```
error: profile manifest cannot be read: JSON parse error
```

**`manifest.nix` (legacy, used by `nix-env`):**
A Nix expression listing installed derivations:
```nix
[
  {
    meta = { ... };
    name = "hello-2.12";
    out = { outPath = "/nix/store/abc-hello-2.12"; };
    outputs = [ "out" ];
    system = "x86_64-linux";
    type = "derivation";
  }
  # ...
]
```
Located at `$profile/manifest.nix`. Only contains packages as store paths; no lock file or flake reference.

**`manifest.json` v1 (new, used by `nix profile`):**
```json
{
  "version": 1,
  "elements": [
    {
      "active": true,
      "attrPath": "packages.x86_64-linux.hello",
      "originalUrl": "flake:nixpkgs",
      "storePaths": ["/nix/store/abc-hello-2.12"],
      "url": "github:NixOS/nixpkgs/7f8d4b088e2...#packages.x86_64-linux.hello"
    }
  ]
}
```

The `nix profile` format records the locked flake URL, allowing `nix profile upgrade` to re-evaluate the flake and update to a newer version. `nix-env` has no concept of flake references — it just stores store paths.

**Safe coexistence:** Use separate profiles. `nix-env --profile ~/.my-legacy-profile -i <pkg>` and `nix profile --profile ~/.my-new-profile install nixpkgs#<pkg>` can coexist as long as they use different profile paths.

---

## Part 3: `builtins.fetchGit` Deep Dive

`builtins.fetchGit` is the Nix builtin for fetching Git repositories. Under the hood, flake inputs of type `git+https:`, `git+ssh:`, and `git+file:` use `builtins.fetchTree { type = "git"; ... }` which calls the same fetcher implementation (`src/libfetchers/git.cc`). `builtins.fetchGit` itself is the older, stable API for the same code path.

### Complete Parameter Reference

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `url` | string | required | Repository URL. Accepts `https://`, `ssh://`, `git@`, `file://`, or relative path |
| `ref` | string | `"HEAD"` | Git reference. Branch names are auto-prefixed with `refs/heads/` unless already starting with `refs/` |
| `rev` | string | tip of `ref` | Exact commit hash (40-char SHA-1). Makes the fetch hermetic |
| `name` | string | `"source"` | Directory name in the Nix store for the exported result |
| `submodules` | boolean | `false` | Whether to recursively fetch all git submodules |
| `shallow` | boolean | `false` | Perform a shallow clone (depth=1). Disables `allRefs` |
| `allRefs` | boolean | `false` | Fetch all refs (`refs/*:refs/*`) to the local cache. Allows resolving `rev` from any ref |
| `lfs` | boolean | `false` | Fetch Git LFS objects |
| `verifyCommit` | boolean | `false` (unless keys given) | Verify commit signature against `publicKey`/`publicKeys` (requires `verified-fetches` feature) |

### Return Value Attributes

```nix
let result = builtins.fetchGit {
  url = "https://github.com/NixOS/nixpkgs.git";
  rev = "7f8d4b088e2...";
  ref = "nixos-unstable";
};
in {
  outPath    = result;           # store path of extracted tree (use in string context)
  rev        = result.rev;       # 40-char commit SHA
  shortRev   = result.shortRev;  # first 7 chars
  revCount   = result.revCount;  # total number of commits in history (not available for shallow)
  lastModified = result.lastModified;  # Unix timestamp of the commit (from git log)
  submodules = result.submodules; # bool: whether submodules were fetched
}
```

`lastModified` is the Unix timestamp of the commit's author date. nixpkgs uses this as `lib.trivial.versionSuffix` to compute version strings like `23.11pre-git.1234567890.abcdef12`. For dirty working trees, `lastModified` is `0` if there is no HEAD commit.

---

### `rev` vs `ref`: Hermetic vs Impure

**`rev` only (hermetic):**
```nix
builtins.fetchGit {
  url = "https://github.com/nixos/nix.git";
  rev = "841fcbd04755c7a2865c51c1e2d3b045976b7452";
}
```
Fetches the exact commit. No branch specified — Nix will look in the default fetch refspec. **This is the only fully hermetic form.** The result is cached and never re-fetched.

**`ref` without `rev` (impure):**
```nix
builtins.fetchGit {
  url = "https://github.com/NixOS/nix.git";
  ref = "master";
}
```
Fetches the current HEAD of `master`. **Impure** — evaluating this expression on different days gives different results. Nix re-fetches periodically based on `tarball-ttl` (default 3600 seconds).

**`ref` + `rev` (hermetic, explicit branch):**
```nix
builtins.fetchGit {
  url = "git@github.com:my-secret/repository.git";
  ref = "master";
  rev = "adab8b916a45068c044658c4158d81878f9ed1c3";
}
```
The `ref` tells Nix which branch to search for the commit. The `rev` pins the result. This is the pattern for private repos where you need to specify the branch explicitly.

---

### `allRefs`: When and Why

By default, `builtins.fetchGit` only fetches the specified `ref` (or `HEAD` if omitted). If you specify a `rev` that is not on the fetched ref's history, Nix fails:

```
error: cannot find Git revision 'abc123' in ref 'refs/heads/main' of repository '...'
```

**When to use `allRefs = true`:**

1. Fetching a commit that is on a non-default branch:
   ```nix
   builtins.fetchGit {
     url = "https://github.com/org/repo.git";
     rev = "abc123...";    # commit is on a feature branch, not main
     allRefs = true;       # fetch refs/*:refs/* so rev can be found anywhere
   }
   ```

2. Fetching a GitHub Pull Request HEAD (not on any branch in the main repo):
   ```nix
   builtins.fetchGit {
     url = "https://github.com/org/repo.git";
     ref = "refs/pull/42/head";   # explicit PR refspec
     allRefs = true;
   }
   ```

3. Fetching by tag when the tag is not on the default branch:
   ```nix
   builtins.fetchGit {
     url = "https://github.com/nixos/nix.git";
     ref = "refs/tags/1.9";  # explicit tag ref; no allRefs needed if only this ref
   }
   ```

**Implementation detail (from `src/libfetchers/git.cc`):**

```cpp
auto fetchRef = getAllRefsAttr(input) ? "refs/*:refs/*"
                : input.getRev() ? input.getRev()->gitRev()
                : /* default HEAD fetch ... */;
repo->fetch(repoUrl.to_string(), fetchRef, shallow);
```

`allRefs = true` translates to the git refspec `refs/*:refs/*` — a full mirror clone of all remote refs into the local cache. This is expensive on large repos (fetches all branches, tags, notes, etc.) but ensures any `rev` is resolvable.

**`allRefs` + `shallow` conflict:**
```
error: cannot use 'shallow' and 'allRefs' simultaneously
```
A shallow clone has no history; `allRefs` requires traversing all refs. They are mutually exclusive.

---

### `submodules = true`: Cost and Mechanics

When `submodules = true`:
1. The main repository is fetched first
2. For each submodule in `.gitmodules`, a **separate** `builtins.fetchGit` call is made recursively
3. Submodule sources are overlaid onto the main tree using a `MountedSourceAccessor`

**Cost:** Each submodule is a separate Git fetch and store path. A repo with 20 submodules means 21 store paths and 21 network fetches. Large monorepos with many submodules can produce closures that are gigabytes larger than without submodules.

**NAR hash changes:** Because the NAR serialization of the result includes submodule content, any submodule update changes the `narHash` and invalidates the entire tree in the Nix store. This makes `submodules = true` hostile to caching.

**Best practice:** Avoid submodules in Nix-packaged code. If unavoidable, pin the exact `rev` of both the main repo and each submodule explicitly.

---

### `shallow = true`: When to Use

Shallow clones fetch only the tip commit (depth=1), not the full history. Tradeoffs:

| | Shallow | Deep |
|--|---------|------|
| Network transfer | Small (only one commit's tree) | Full history |
| `revCount` | Not available (error if accessed) | Available |
| `allRefs` | Incompatible | Compatible |
| Reproducibility | Same when `rev` is pinned | Same when `rev` is pinned |
| Re-fetch on `rev` change | No cached history to check | Can check locally |

Use `shallow = true` for fetching dependencies where you only need the source tree, not git history. This is the default behavior for `github:` flake inputs (which use archive downloads, not git at all).

---

### Git Cache Location

From `src/libfetchers/git.cc`:

```cpp
std::filesystem::path getCachePath(std::string_view key, bool shallow)
{
    auto name = hashString(HashAlgorithm::SHA256, key)
        .to_string(HashFormat::Nix32, false) + (shallow ? "-shallow" : "");
    return getCacheDir() / "gitv3" / std::move(name);
}
```

The cache is at `~/.cache/nix/gitv3/<sha256-of-url>[-shallow]/`. Each URL gets its own directory, named by SHA-256 of the URL (in Nix32 encoding). Shallow and non-shallow fetches of the same URL use separate cache directories.

**Invalidation:**
- `nix flake update` — re-fetches the remote, writes to the same cache directory with new content
- The `tarball-ttl` nix.conf setting (default 3600 seconds) controls how long before Nix checks if the remote has changed for unfixed `ref` fetches
- Manual cache clearing: `rm -rf ~/.cache/nix/gitv3/` — safe, Nix will re-fetch on next use
- There is no `nix cache prune git` command; GC does not collect git caches (they are not in the Nix store, so GC does not manage them)

---

### Dirty Working Tree Behavior

When `url = ./.` (or any local path with uncommitted changes):

```nix
builtins.fetchGit ./work-dir
```

The fetcher checks for dirty state. From `src/libfetchers/git.cc`:

```cpp
input.attrs.insert_or_assign("dirtyRev",
    repoInfo.workdirInfo.headRev->gitRev() + "-dirty");
input.attrs.insert_or_assign("dirtyShortRev",
    repoInfo.workdirInfo.headRev->gitShortRev() + "-dirty");
```

- `result.rev` is **not set** (or throws) for dirty trees — there is no canonical commit
- `result.dirtyRev` is set to `<headCommitHash>-dirty`
- `result.dirtyShortRev` is set to `<shortHash>-dirty`
- `result.lastModified` is `0` if there is no HEAD commit, or the HEAD commit timestamp if one exists

The `allowDirty` setting (nix.conf) controls whether dirty trees are permitted (`true` by default in interactive use, `false` in some CI contexts). The `warnDirty` setting (default `true`) prints a warning when a dirty tree is used.

**For local source development, prefer `builtins.path` over `builtins.fetchGit ./.`:**

```nix
# builtins.path filters via a filter function and always produces a clean store path
builtins.path {
  path = ./.;
  filter = path: type: !(lib.hasSuffix ".git" path);
}

# Or use lib.fileset (higher-level):
lib.fileset.toSource {
  root = ./.;
  fileset = lib.fileset.gitTracked ./.;
}
```

`builtins.path` copies the path into the Nix store with NAR hashing — the result always has a definitive store path regardless of git dirty state.

---

### `builtins.fetchGit` vs `builtins.fetchTree` vs Flake Inputs

| | `builtins.fetchGit` | `builtins.fetchTree { type="git"; }` | Flake git input |
|-|---------------------|---------------------------------------|-----------------|
| Experimental feature | None (stable builtin) | `fetch-tree` feature | `flakes` feature |
| Lock file integration | No | No | Yes (via `flake.lock`) |
| `narHash` written to result | No | Yes | Yes |
| Calling convention | Named attrs or URL string | Attr set with `type` field | `inputs.<name>.url = "git+https:..."` |
| Underlying fetcher | `src/libfetchers/git.cc` | Same | Same |
| `lastModified` in result | Yes | Yes | Yes (via lock node) |

**The `flakes` feature gate on `builtins.fetchTree`:**

`builtins.fetchTree` is artificially gated behind the `flakes` experimental feature (issue [NixOS/nix#5541](https://github.com/NixOS/nix/issues/5541)), even though it is useful independently of flakes. `builtins.fetchGit` has no such gating — it is available in all Nix builds. If you need fetchTree semantics in a non-flake context without enabling flakes, use `builtins.fetchGit`.

---

### Practical Patterns

**Pinning a private GitHub dependency:**
```nix
let
  mylib = builtins.fetchGit {
    url = "git@github.com:myorg/mylib.git";
    rev = "abc123def456abc123def456abc123def456abc1";  # 40-char SHA
    ref = "main";  # optional but helps if rev is on a non-default branch
  };
in
  import "${mylib}/default.nix" { inherit pkgs; }
```

**Fetching a specific GitHub Actions workflow artifact (non-default branch):**
```nix
builtins.fetchGit {
  url = "https://github.com/org/repo.git";
  rev = "the-pr-merge-commit-sha";
  allRefs = true;  # PR heads are not on refs/heads/*
}
```

**Local development — always use `builtins.path` for cleaner semantics:**
```nix
let
  src = builtins.path {
    name = "my-project-source";
    path = ./.;
    # Filter excludes .git, node_modules, target/, etc.
    filter = path: type:
      !lib.any (p: lib.hasPrefix (toString ./. + "/" + p) path) [
        ".git" "node_modules" "target" "_build"
      ];
  };
in ...
```

**Checking `lastModified` for versioning (nixpkgs pattern):**
```nix
let
  src = builtins.fetchGit {
    url = "https://github.com/NixOS/nixpkgs.git";
    rev = "7f8d4b...";
  };
  # Convert Unix timestamp to date string for version
  date = builtins.substring 0 8 (builtins.readFile
    (pkgs.runCommand "date" {} "date -d @${toString src.lastModified} +%Y%m%d > $out"));
in "unstable-${date}"
```

---

## Cross-Cutting: Why `lib.fileset` / `lib.cleanSource` Matters for Store Paths

Since source store paths depend on the NAR of the entire source tree, any unrelated file (editor temp files, built artifacts, test output) included in the source will invalidate the downstream build cache. This is the motivation for source filtering:

```nix
# Without filtering: .git/, result symlinks, node_modules/ all included in NAR
# → changes to ANY of these invalidate ALL downstream derivations

# With lib.fileset (cleanest):
lib.fileset.toSource {
  root = ./.;
  fileset = lib.fileset.unions [
    ./src
    ./package.json
    ./go.mod
    ./go.sum
  ];
}

# With lib.cleanSourceWith (older API):
lib.cleanSourceWith {
  src = ./.;
  filter = path: type:
    lib.cleanSourceFilter path type &&
    !(lib.hasSuffix ".nix" path);  # exclude Nix files from build inputs
}
```

Each filtering approach changes which files enter the NAR serialization, directly controlling which source store path is produced and how frequently downstream build caches are invalidated.
