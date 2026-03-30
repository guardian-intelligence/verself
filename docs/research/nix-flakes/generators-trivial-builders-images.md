# Nix: `lib.generators`, `pkgs.formats`, Trivial Builders, Disk Images, and `nix flake check` Internals

Covers four interrelated topics:
1. `lib.generators` — rendering config files from Nix values
2. `pkgs.formats` — higher-level format objects for NixOS modules
3. `pkgs.runCommand` family and `pkgs.writeText` family — lightweight derivations
4. NixOS disk image and ISO generation
5. `nix flake check` deep dive (extends [outputs-and-ci.md](outputs-and-ci.md))

---

## Topic 1: `lib.generators` — Config File Rendering

**Source:** `lib/generators.nix` in nixpkgs

`lib.generators` is the low-level rendering layer. It converts Nix values to text strings in various config file formats. These functions return strings — they do not create store paths. `pkgs.formats` (Topic 2) wraps them to produce store paths.

### `mkValueStringDefault` and `mkKeyValueDefault`

These are the primitive building blocks used by all key-value renderers.

**`mkValueStringDefault`** converts a single Nix value to a string for use as a config file value:

```nix
# Signature:
mkValueStringDefault = { }: v:
# Handles:
# - int → toString
# - float → builtins.toJSON (preserves precision; avoids toString rounding)
# - string → identity (no quoting added)
# - bool → "true" / "false"
# - null → "null"
# - derivation → toString (the outPath)
# Errors on: list, attrset, function
```

Floats use `builtins.toJSON` rather than `toString` because `toString 1.5` gives `"1.5"` but `toString 0.1` can give `"0.10000000000000001"` — JSON serialization is more precise.

**`mkKeyValueDefault`** wraps `mkValueStringDefault` into a line renderer:

```nix
# Signature:
mkKeyValueDefault = { mkValueString ? mkValueStringDefault { } }: sep: k: v
# Produces: "${escape [sep] k}${sep}${mkValueString v}"
# sep appearing in k is backslash-escaped automatically
```

Customization point: supply your own `mkValueString` to handle format-specific quoting (e.g., INI values that need quoting when they contain `=`).

### `toKeyValue`

Flat `key = value` format. Used for environment files, simple config formats.

```nix
# Signature:
toKeyValue = {
  mkKeyValue ? mkKeyValueDefault { } "=",
  listsAsDuplicateKeys ? false,   # list [a b c] → "key=a\nkey=b\nkey=c\n"
  indent ? ""                      # prefix each line
}: attrs
```

Example — generating a systemd `EnvironmentFile`:
```nix
lib.generators.toKeyValue { } {
  NODE_ENV = "production";
  PORT = 3000;
  LOG_LEVEL = "warn";
}
# → "LOG_LEVEL=warn\nNODE_ENV=production\nPORT=3000\n"
# Note: attrsets are sorted alphabetically by key
```

`listsAsDuplicateKeys` is essential for formats like `sshd_config` where the same key appears multiple times:
```nix
lib.generators.toKeyValue {
  listsAsDuplicateKeys = true;
  mkKeyValue = lib.generators.mkKeyValueDefault { } " ";  # space separator
} {
  AllowUsers = [ "alice" "bob" ];
  Port = 22;
}
# → "AllowUsers alice\nAllowUsers bob\nPort 22\n"
```

### `toINI`

Two-level `[section]` / `key = value` format. The outer attrset is sections; the inner attrset is key-value pairs within each section.

```nix
# Signature:
toINI = {
  mkSectionName ? (name: escape [ "[" "]" ] name),  # escape brackets in section names
  mkKeyValue ? mkKeyValueDefault { } "=",
  listsAsDuplicateKeys ? false
}: attrsOfAttrs
```

Example:
```nix
lib.generators.toINI { } {
  database = { host = "localhost"; port = 5432; };
  server = { bind = "0.0.0.0"; workers = 4; };
}
# → "[database]\nhost=localhost\nport=5432\n\n[server]\nbind=0.0.0.0\nworkers=4\n"
```

How `services.openssh.settings` uses it: the `sshd` NixOS module uses `toKeyValue` (not `toINI`) because `sshd_config` is flat. INI is used for services like `systemd` unit files, `gitconfig`, `php.ini`, `mumble-server.ini`.

**`toINIWithGlobalSection`** — for formats that have key-value pairs before any section header:

```nix
# Signature:
toINIWithGlobalSection = { mkSectionName, mkKeyValue, listsAsDuplicateKeys }:
  { globalSection, sections ? {} }
```

Example — `git config` style where `[core]` is separate from top-level keys:
```nix
lib.generators.toINIWithGlobalSection { } {
  globalSection = { autocrlf = false; };
  sections = {
    user = { name = "Alice"; email = "alice@example.com"; };
  };
}
# → "autocrlf=false\n\n[user]\nemail=alice@example.com\nname=Alice\n"
```

### `toGitINI`

Specialization of INI for git config format. Key differences from plain `toINI`:
- Values are **indented with a tab**: `\t key = value`
- Supports **nested sections** via subsection notation: `[section "subsection"]`
- Special value escaping: newlines → `\n`, tabs → `\t`, backslash → `\\`, double-quote → `\"`

Used by `programs.git.extraConfig` in Home Manager and the `gitIni` `pkgs.formats` variant.

### `toPretty`

Human-readable Nix value printer. Not a config file format — used for error messages, documentation generation, and `nix repl` display. Produces Nix-syntax output.

```nix
# Signature:
toPretty = {
  allowPrettyValues ? false,   # respect __pretty on attrsets
  multiline ? true,            # indent nested structures
  indent ? ""                  # initial indentation
}: value
```

Key behaviors:
- **Functions**: prints argument information — `<function>` or `<function, args: {a, b?, c?}>` using `builtins.functionArgs`
- **Derivations**: renders as `<derivation foo-1.0>` (uses `v.name or "???"`)
- **Floats**: uses `builtins.toJSON` to avoid precision loss from `toString`
- **null**: renders as the string `"null"` (Nix syntax)
- **Lists**: `[ a b c ]` with newlines and indentation when `multiline = true`
- **Attrsets**: `{ key = value; }` — when `allowPrettyValues = true`, respects `__pretty` attribute for custom renderers

Design note from source: "The pretty-printed string should be suitable for rendering default values in the NixOS manual." NixOS module documentation uses `toPretty` to render option defaults.

### `withRecursion`

Wraps any generator to handle recursive attrsets safely:

```nix
# Signature:
withRecursion = {
  depthLimit,              # required; max nesting depth
  throwOnDepthLimit ? true # false → return placeholder string instead of throw
}: value
```

Protects against infinite recursion from self-referential attrsets. Skips special attributes `__functor` and `__toString` to avoid triggering side effects during traversal.

### `toYAML` / `toJSON`

Both delegate to `lib.strings.toJSON` (which wraps `builtins.toJSON`). Rationale from source: "YAML has been a strict superset of JSON since 1.2, so we use toJSON."

```nix
lib.generators.toYAML { } { server = { port = 8080; }; }
# → '{"server":{"port":8080}}'
```

**Limitations of this approach**:
- No YAML-specific features: no comments, no multi-line strings (`|`/`>`), no YAML anchors/aliases
- YAML 1.1 parsers (PyYAML default, many Go parsers pre-gopkg.in/yaml.v3) treat `true`/`false`/`yes`/`no` as booleans — this can cause surprising behavior
- Null in YAML 1.1 vs 1.2: `~` and `null` behave differently across parser versions
- For real YAML with formatting, use `pkgs.formats.yaml { }` which pipes through `remarshal` for proper YAML serialization

### `toDHALL`

Converts Nix values to Dhall notation:

```nix
lib.generators.toDHALL { } { x = 1; y = "hello"; }
# → "{ x = +1, y = \"hello\" }"
```

Constraints:
- All integers map to Dhall `Integer` type (never `Natural` — Dhall `Natural` cannot represent negatives, so `+` prefix is always added)
- `null` values cause `abort` — no Dhall equivalent
- Functions cause `abort`

### `toLua`

Converts Nix to Lua syntax, used for Neovim plugin configurations (nixvim) and other Lua-configured tools.

```nix
lib.generators.toLua { } { plugins = [ "telescope" "nvim-tree" ]; timeout = 1000; }
```

Special: `lib.generators.mkLuaInline "vim.fn.stdpath('data')"` produces an object tagged `{ _type = "lua-inline"; expr = ...; }` that `toLua` inserts verbatim — for raw Lua expressions that cannot be represented as Nix values.

Options: `asBindings` (output as `key = value` assignments rather than a table), `multiline`, `columnWidth`, `indentWidth`, `indentUsingTabs`.

### `toPlist`

Apple Property List (XML) format. Used for macOS launchd agents, Info.plist, and macOS preferences.

```nix
lib.generators.toPlist { escape = true; } { Label = "com.example.agent"; }
```

The `escape = true` option enables XML entity escaping (`<`, `>`, `&`, `"`, `'`). Using `escape = false` (old default) emits a deprecation warning in recent nixpkgs. Wraps output in standard plist DOCTYPE header. Filters `_module` attributes (module system internals) and null values.

---

## Topic 2: `pkgs.formats` — Higher-Level Config File Objects

**Source:** `pkgs/pkgs-lib/formats.nix` in nixpkgs

`pkgs.formats` wraps `lib.generators` to produce **store paths** rather than strings. The design follows RFC 42: each format function returns a record `{ type; generate; lib? }`.

### The Return Value Shape

Every `pkgs.formats.<name> {}` call returns:

```
{
  type    # lib.types.* value for use in mkOption
  generate  # name: value → storePath (a derivation)
  lib?    # optional format-specific helpers
}
```

- **`type`**: the NixOS module system type to use in `lib.mkOption { type = format.type; }`. Validates that the Nix value can be serialized to this format.
- **`generate name value`**: builds a derivation whose `$out` is a single file containing `value` serialized to the format. `name` becomes the filename in the store path.
- **`lib`**: format-specific helper functions (e.g., `elixirConf.lib.mkRaw`, `luaConf.lib.mkRaw`)

### Core Formats

**`pkgs.formats.json { }`**

```nix
format = pkgs.formats.json { };
configFile = format.generate "app.json" { port = 8080; debug = false; };
# /nix/store/<hash>-app.json  ← contains {"debug":false,"port":8080}
```

Internally: `builtins.toJSON value | jq` (jq validates and pretty-prints). Type: `types.json` (any JSON-serializable value).

**`pkgs.formats.yaml { }`**

Alias for `yaml_1_1 { }`. Uses `remarshal_0_17` (Python) to convert JSON→YAML. Produces properly formatted YAML with line breaks and indentation — unlike `lib.generators.toYAML` which produces inline JSON-compatible YAML.

`yaml_1_2 { }` variant uses `remarshal` (newer) for YAML 1.2 compliance. The difference matters for edge cases like `yes`/`no` booleans and octal literals.

**`pkgs.formats.toml { }`**

Uses `remarshal` to convert JSON→TOML. Type: `types.toml`. TOML requires integer/float/string/boolean distinction that JSON preserves, so this round-trip is safe.

**`pkgs.formats.ini { listsAsDuplicateKeys? listToValue? atomsCoercedToLists? }`**

Wraps `lib.generators.toINI`. Returns `{ type = attrsOf (iniSection atom); lib.types.atom; generate; }`.

`atom` type is `nullOr (oneOf [ bool int float str ])` — the set of scalar types that INI can represent.

```nix
format = pkgs.formats.ini { listsAsDuplicateKeys = true; };
```

**`pkgs.formats.iniWithGlobalSection { ... }`**

For INI formats with a global section before named sections. Type is a submodule:
```nix
{
  globalSection = lib.mkOption { type = attrsOf atom; default = {}; };
  sections = lib.mkOption { type = attrsOf (iniSection atom); default = {}; };
}
```

**`pkgs.formats.keyValue { listsAsDuplicateKeys? listToValue? }`**

Flat `key=value` format. Type: `attrsOf atom`.

**`pkgs.formats.gitIni { listsAsDuplicateKeys? }`**

Git config format with tab indentation and subsection support. Type: `attrsOf (attrsOf (either atom (attrsOf atom)))` — three levels for `[section "subsection"]` nesting.

### The `settingsFormat` Pattern for NixOS Service Modules

This is the **recommended pattern** for writing NixOS modules that accept arbitrary service configuration. It enables:
- Type-checked Nix values
- Automatic serialization to the service's native config format
- `lib.mkDefault` precedence for module-provided defaults
- `lib.mkForce` overrides for users

**Full pattern:**

```nix
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.foo;
  # 1. Choose a format matching the service's config file format
  settingsFormat = pkgs.formats.toml { };
in
{
  options.services.foo = {
    enable = lib.mkEnableOption "foo service";

    # 2. Expose settings as a typed option
    settings = lib.mkOption {
      # Use format.type so the module system validates serializability
      type = settingsFormat.type;
      default = { };
      description = "Configuration for foo, serialized to /etc/foo/config.toml";
    };
  };

  config = lib.mkIf cfg.enable {
    # 3. Provide module-level defaults (users can override with mkForce)
    services.foo.settings = {
      log_level = lib.mkDefault "warn";
      data_dir = lib.mkDefault "/var/lib/foo";
    };

    # 4. Generate the config file using format.generate
    environment.etc."foo/config.toml".source =
      settingsFormat.generate "foo-config.toml" cfg.settings;

    systemd.services.foo = {
      wantedBy = [ "multi-user.target" ];
      serviceConfig.ExecStart = "${pkgs.foo}/bin/foo --config /etc/foo/config.toml";
    };
  };
}
```

**Combining with `freeformType`** for partial type-checking — validate specific known keys while still accepting arbitrary extras:

```nix
settings = lib.mkOption {
  type = lib.types.submodule {
    # freeformType: catch-all for unknown keys
    freeformType = settingsFormat.type;

    # Explicitly typed known options (get documentation + validation)
    options.port = lib.mkOption {
      type = lib.types.port;
      default = 8080;
      description = "TCP port to listen on";
    };
    options.log_level = lib.mkOption {
      type = lib.types.enum [ "debug" "info" "warn" "error" ];
      default = "warn";
    };
  };
  default = { };
};
```

Real examples in nixpkgs using this pattern:
- `services.caddy`: `settingsFormat = pkgs.formats.json { }; configFile = settingsFormat.generate "caddy.json" cfg.settings`
- `services.clickhouse`: `format = pkgs.formats.yaml { }; serverConfigFile = format.generate "config.yaml" cfg.serverConfig`
- `services.grafana`: `pkgs.formats.ini { }`
- `services.prometheus`: `pkgs.formats.json { }`

### Extended Formats

`pkgs.formats` also provides specialized formats for specific ecosystems. These are less commonly used but cover niche cases:

| Format | Use case | Notable `lib` helpers |
|--------|----------|----------------------|
| `javaProperties` | Java `.properties` files | — |
| `libconfig` | libconfig format (C/C++ config) | `mkHex`, `mkOctal`, `mkFloat`, `mkArray`, `mkList` |
| `hocon` | Lightbend HOCON (Akka, Play) | `mkInclude`, `mkAppend`, `mkSubstitution` |
| `elixirConf` | Elixir Config DSL (runs `mix format`) | `mkRaw`, `mkGetEnv`, `mkAtom`, `mkTuple`, `mkMap` |
| `lua` | Lua table/binding format (runs `stylua`) | `mkRaw` (raw Lua injection via `lib.mkLuaInline`) |
| `nixConf` | `nix.conf` key-value with Nix validation | — |
| `pythonVars` | Python variable assignments (runs `black`) | `mkRaw` |
| `xml` | XML via `xmltodict` + `xmllint` validation | — |
| `plist` | Apple Property List XML | — |
| `systemd` | systemd unit files (extends `ini`) | — |
| `hcl1` | HashiCorp Configuration Language v1 | — |

The `elixirConf`, `lua`, and `pythonVars` formats invoke language-specific formatters at build time, which means their closures pull in those language runtimes as build-time dependencies.

---

## Topic 3: `pkgs.runCommand` Family and Write Helpers

**Source:** `pkgs/build-support/trivial-builders/default.nix`

These are the most-used helpers for creating small derivations without writing full `stdenv.mkDerivation` calls.

### `runCommandWith` — the Underlying Primitive

All `runCommand*` variants call this:

```nix
runCommandWith = {
  stdenv ? stdenv,         # which stdenv to use
  runLocal ? false,        # true → preferLocalBuild + allowSubstitutes = false
  derivationArgs ? { },    # extra mkDerivation attributes
  name                     # derivation name (required)
}: buildCommand
```

`buildCommand` is the shell script. It is passed via `passAsFile` (written to a temp file rather than an environment variable) to avoid the Linux environment variable size limit. The script **must create `$out`**; failing to do so causes the build to fail with a generic error.

### `runCommand` — The Standard Choice

```nix
runCommand = name: env: runCommandWith {
  stdenv = stdenvNoCC;   # ← NO C compiler
  runLocal = false;
  inherit name;
  derivationArgs = env;
};
```

**Important**: `runCommand` uses `stdenvNoCC`, not `stdenv`. This means:
- No `gcc`, `cc`, `g++`, `ld` in PATH
- No `NIX_CFLAGS_COMPILE`, `NIX_LDFLAGS`
- Smaller build-time closure
- If you need C compilation in the script, use `runCommandCC` instead

The `env` parameter is an attrset where each key becomes an environment variable. Values must be coercible to strings. Derivations in `env` become their `outPath`.

```nix
pkgs.runCommand "process-data" {
  src = ./data.json;                 # → env var $src = /nix/store/.../data.json
  nativeBuildInputs = [ pkgs.jq ];  # tool available in PATH during build
} ''
  mkdir $out
  jq '.items[]' $src > $out/items.json
''
```

Note: `nativeBuildInputs` is the correct attribute name for tools needed during the build. The legacy name `buildInputs` also works here since cross-compilation isn't involved, but `nativeBuildInputs` is semantically correct.

### `runCommandLocal`

Forces local execution — bypasses remote builders and substituters:

```nix
runCommandLocal = name: env: runCommandWith {
  stdenv = stdenvNoCC;
  runLocal = true;   # → preferLocalBuild = true; allowSubstitutes = false
  inherit name;
  derivationArgs = env;
};
```

When to use `runCommandLocal`:
- Scripts that complete in milliseconds — the overhead of querying a binary cache or remote builder exceeds the build time
- Scripts that depend on local state (rarely correct — try to avoid this)
- `checks` derivations in flakes that run `nixfmt --check` or `shellcheck` — faster than cache round-trip

When NOT to use `runCommandLocal`:
- Anything that takes more than ~1 second — remote builders can parallelize this
- Anything you want shared across machines — `allowSubstitutes = false` means teammates can't get this from the cache

### `runCommandCC`

Like `runCommand` but uses the full `stdenv` with C compiler:

```nix
runCommandCC = name: env: runCommandWith {
  stdenv = stdenv;   # ← includes gcc, cc, binutils
  ...
};
```

Use only when the build script actually invokes a C compiler. Pulling in the full stdenv adds ~100MB to the build-time closure.

### `writeText` and `writeTextFile`

**`writeText name text`** — the simplest case:

```nix
pkgs.writeText "my-config" ''
  setting=value
  debug=false
''
# → /nix/store/<hash>-my-config  (a single file)
```

Note: the `text` argument must be a string. Passing a non-string triggers a deprecation warning (it used to work via `toString` coercion, but this is being removed).

**`writeTextFile`** — full options:

```nix
writeTextFile = {
  name,                      # derivation name (required)
  text,                      # file content (required)
  executable ? false,        # chmod +x
  destination ? "",          # "/bin/myscript" → $out/bin/myscript
                             # empty → $out is the file itself
  checkPhase ? "",           # shell commands run after writing, before output
  meta ? { },
  passthru ? { },
  allowSubstitutes ? false,  # default: local build preferred
  preferLocalBuild ? true,
  derivationArgs ? { }
}
```

When `destination` is non-empty, `$out` is a directory and the file lives at `$out/${destination}`. When `destination` is empty, `$out` is the file itself.

`checkPhase` is the canonical way to validate generated files:
```nix
pkgs.writeTextFile {
  name = "my-script";
  text = ''
    #!/usr/bin/env bash
    echo "hello"
  '';
  executable = true;
  destination = "/bin/my-script";
  checkPhase = ''
    ${pkgs.shellcheck}/bin/shellcheck "$target"
  '';
}
```

The `$target` variable in `checkPhase` refers to the file that was just written.

### `writeTextDir`, `writeScript`, `writeShellScript`, `writeShellScriptBin`

These are thin wrappers over `writeTextFile`:

```nix
# Creates $out/share/my-file
writeTextDir "share/my-file" "content"
# ≡ writeTextFile { name = "my-file"; text = "content"; destination = "/share/my-file"; }

# Creates an executable $out (just chmod +x, no shebang)
writeScript "my-script" "echo hi"
# ≡ writeTextFile { name = "my-script"; text = "echo hi"; executable = true; }

# Adds #!/bin/sh shebang + shell dry-run syntax check
writeShellScript "my-script" "echo hi"
# ≡ writeTextFile {
#     name = "my-script";
#     executable = true;
#     text = "#!${runtimeShell}\necho hi";
#     checkPhase = "${stdenv.shellDryRun} \"$target\"";
#   }

# Like writeShellScript but places at /bin/name (adds to PATH when put in buildInputs)
writeShellScriptBin "my-script" "echo hi"
# ≡ writeTextFile {
#     name = "my-script";
#     executable = true;
#     text = "#!${runtimeShell}\necho hi";
#     destination = "/bin/my-script";
#     checkPhase = "${stdenv.shellDryRun} \"$target\"";
#   }
```

`writeShellScript` vs `writeShellApplication`: `writeShellApplication` (covered in [tooling-and-patterns.md](tooling-and-patterns.md)) adds `set -euo pipefail`, runs `shellcheck`, and can declare `runtimeInputs` that get added to PATH at runtime. Use `writeShellApplication` for production scripts; `writeShellScript` for simple glue that doesn't need that overhead.

**`runtimeShell`** is `pkgs.runtimeShell` — the bash from nixpkgs, not `/bin/sh`. On NixOS, `/bin/sh` is busybox ash; on macOS, it's the system zsh. Using `pkgs.runtimeShell` ensures reproducibility.

### `linkFarm`

Creates a store path that is a directory of symlinks:

```nix
# Signature:
linkFarm = name: entries
# entries can be:
#   a list: [ { name = "bin/foo"; path = drv; } ... ]
#   an attrset: { "bin/foo" = drv; }
```

Example — creating a custom `bin` directory:
```nix
pkgs.linkFarm "my-tools" [
  { name = "bin/hello"; path = "${pkgs.hello}/bin/hello"; }
  { name = "bin/jq"; path = "${pkgs.jq}/bin/jq"; }
  { name = "share/my-config"; path = pkgs.writeText "cfg" "key=val"; }
]
```

The resulting derivation has `$out/bin/hello` → store path of hello, etc. Unlike `symlinkJoin` which copies entire directory trees, `linkFarm` creates exactly the links you specify.

Internally: `runCommand` with `preferLocalBuild = true; allowSubstitutes = false` that calls `mkdir -p` for each unique parent directory and then `ln -s` for each entry.

When `entries` is an attrset, duplicate names are resolved with last-wins semantics (attrset merge order). When a list, the list order determines precedence.

`passthru.entries` on the output derivation exposes the entries list for introspection.

### `symlinkJoin`

Merges multiple derivations into one store path by symlinking all their contents:

```nix
# Signature:
symlinkJoin = {
  name,                       # or pname + version
  paths,                      # list of derivations
  postBuild ? "",             # shell commands after symlinking
  stripPrefix ? "",           # remove this prefix from all paths before linking
  failOnMissing ? (stripPrefix != ""),  # error if stripPrefix dir missing
  preferLocalBuild ? true,
  allowSubstitutes ? false,
  ...
}
```

Example:
```nix
pkgs.symlinkJoin {
  name = "my-combined-tools";
  paths = [ pkgs.hello pkgs.cowsay pkgs.jq ];
  postBuild = ''
    wrapProgram $out/bin/hello --set GREETING "hi"
  '';
}
```

**Implementation note** (corrects a common misconception): `symlinkJoin` does **not** wrap `buildEnv`. It uses `lndir` directly to create symlink forests. The build script iterates through input paths and calls `lndir -silent $path $out` for each. This means:
- It does NOT use buildEnv's priority system
- Collision handling: last path in the list wins (no priority numbers)
- It does NOT have `pathsToLink`, `extraOutputsToInstall`, or `ignoreCollisions` parameters
- The `stripPrefix` option removes a path prefix from each input before linking (e.g., `stripPrefix = "/lib/tmpfiles.d"` to collect only that subdirectory from multiple packages)

**`buildEnv` vs `symlinkJoin` comparison:**

| Feature | `buildEnv` | `symlinkJoin` |
|---------|-----------|--------------|
| Collision handling | Priority system (meta.priority) | Last-wins (list order) |
| `pathsToLink` | Yes — filter to subdirs | No |
| `extraOutputsToInstall` | Yes | No |
| `postBuild` | Yes | Yes |
| `stripPrefix` | No | Yes |
| Implementation | Perl script | `lndir` |
| Use case | Profile environments, `nix profile` | Combining a few packages |

For `forge-metal`'s `server-profile`, `buildEnv` with explicit `pathsToLink` is appropriate — it surfaces collisions immediately and supports the priority system. `symlinkJoin` would silently shadow conflicting files.

---

## Topic 4: NixOS Disk Image and ISO Generation

### The `system.build` Namespace

`nixosConfigurations.<name>.config.system.build` is an attrset of derivations produced by various NixOS modules. Key outputs:

| Attribute | Produced by | Description |
|-----------|------------|-------------|
| `toplevel` | `nixos/modules/system/build.nix` | The complete system closure; what `nixos-rebuild switch` builds |
| `isoImage` | `nixos/modules/installer/cd-dvd/iso-image.nix` | Bootable ISO file |
| `sdImage` | ARM SD card modules | Raw SD card image for Raspberry Pi etc. |
| `diskoImages` | disko | Pre-partitioned disk images with disko layout |
| `image` | `nixos/modules/image/repart.nix` | Compressed disk image via systemd-repart |

Building `toplevel` directly:
```bash
# From flake
nix build .#nixosConfigurations.myhost.config.system.build.toplevel

# Is equivalent to
nixos-rebuild build --flake .#myhost
```

### `system.build.isoImage`

Activated by importing `nixpkgs/nixos/modules/installer/cd-dvd/iso-image.nix`.

Key `isoImage.*` options:

| Option | Default | Description |
|--------|---------|-------------|
| `isoImage.squashfsCompression` | `"zstd -Xcompression-level 19"` | squashfs compression for the Nix store layer |
| `isoImage.compressImage` | `false` | Apply zstd compression to the outer ISO |
| `isoImage.volumeID` | `"nixos-[edition]-[release]-[arch]"` | ISO volume label (max 32 chars per ISO 9660) |
| `isoImage.makeBiosBootable` | `true` (x86) | BIOS/legacy boot support |
| `isoImage.makeEfiBootable` | `false` | UEFI boot support |
| `isoImage.makeUsbBootable` | `false` | USB boot support (hybrid ISO) |
| `isoImage.contents` | `[]` | Extra files to copy to fixed paths in the ISO |
| `isoImage.storeContents` | `[]` | Extra derivations to include in the Nix store inside the ISO |
| `isoImage.includeSystemBuildDependencies` | `false` | Include all build-time deps (significantly increases size) |

Flake integration:
```nix
# flake.nix
{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }: {
    nixosConfigurations.installer = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        "${nixpkgs}/nixos/modules/installer/cd-dvd/installation-cd-minimal.nix"
        {
          # Customize the ISO
          isoImage.squashfsCompression = "zstd -Xcompression-level 6";  # faster build
          isoImage.makeEfiBootable = true;
          isoImage.makeUsbBootable = true;
          # Add extra packages to the live environment
          environment.systemPackages = [ nixpkgs.legacyPackages.x86_64-linux.vim ];
        }
      ];
    };

    packages.x86_64-linux.installer-iso =
      self.nixosConfigurations.installer.config.system.build.isoImage;
  };
}
```

Build:
```bash
nix build .#packages.x86_64-linux.installer-iso
# → result/iso/nixos-*.iso
```

### `make-disk-image.nix` — the Raw Disk Image Builder

`nixpkgs/nixos/lib/make-disk-image.nix` is a function (not a module) that builds raw disk images. Used internally by `system.build.diskoImages` and many nixos-generators formats.

Key parameters:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `diskSize` | `"auto"` | MiB; `"auto"` = calculated from closure + `additionalSpace` |
| `additionalSpace` | `"512M"` | Extra space beyond auto-calculated size |
| `bootSize` | `"256M"` | Boot partition size (for efi/hybrid layouts) |
| `partitionTableType` | `"legacy"` | Partition layout: `legacy`, `legacy+boot`, `legacy+gpt`, `efi`, `efixbootldr`, `hybrid`, `none` |
| `fsType` | `"ext4"` | Root filesystem type |
| `format` | `"raw"` | Output format: `raw`, `qcow2`, `qcow2-compressed`, `vdi`, `vpc` |
| `installBootLoader` | `true` | Run `switch-to-configuration` during image creation |
| `contents` | `[]` | `[ { source; target; mode?; user?; group?; } ]` files to copy |
| `additionalPaths` | `[]` | Extra store paths to copy to the image |
| `onlyNixStore` | `false` | Copy only the Nix store (no bootloader, no `/etc`) |
| `deterministic` | `true` | Fix all UUIDs for reproducible output |
| `copyChannel` | `true` | Include a Nix channel inside the image |
| `memSize` | `1024` | VM memory for the build (builds run inside a VM) |

`make-disk-image.nix` runs QEMU internally to populate the image — it needs KVM access at build time. This is why building disk images on CI requires a `kvm` system feature or a Linux builder.

### `nixos-rebuild build-image` (NixOS 25.05+)

`nixos-rebuild build-image` is the modern interface, replacing `nixos-generate` from the nix-community/nixos-generators project. As of NixOS 25.05, most image formats have been upstreamed into nixpkgs.

```bash
# List available image variants
nixos-rebuild build-image --list

# Build a specific variant
nixos-rebuild build-image --image-variant amazon --flake .#myhost

# Build from a non-flake NixOS config
nixos-rebuild build-image --image-variant iso
```

Supported image variants (as of 25.05):
- `iso` — bootable installer ISO
- `amazon` — AWS AMI
- `azure` — Azure VHD
- `digitalocean` — DigitalOcean droplet image
- `gce` — Google Compute Engine image
- `kexec` — kexec tarball for remote bare-metal takeover
- `lxc` — LXC container tarball
- `proxmox` — Proxmox VE image
- `qcow2` — QEMU/KVM disk image
- `raw` — raw disk image
- `sd-card` — ARM SD card image

The `system.build.images` attrset (added 25.05) provides all images as flake-accessible derivations:
```nix
packages.x86_64-linux.amazon-image =
  self.nixosConfigurations.myhost.config.system.build.images.amazon;
```

**Per-variant customization** via `image.modules`:
```nix
# In NixOS configuration
image.modules.linode = {
  # Override settings only for the linode image variant
  boot.loader.systemd-boot.enable = lib.mkForce false;
  boot.loader.grub.enable = true;
};
```

### `system.build.image` via `nixos/modules/image/repart.nix`

The repart module uses `systemd-repart` to build disk images declaratively. Unlike `make-disk-image.nix`, this does NOT require a VM/KVM — it runs on the host using `systemd-repart`'s userspace mode.

Key `image.repart.*` options:
- `image.repart.name` / `image.repart.version` — image identification
- `image.repart.compression` — algorithm (`zstd`, `xz`, `zstd-seekable`) + level
- `image.repart.seed` — UUID seed for reproducible partition UUIDs
- `image.repart.partitions` — attrset of partition configs with `storePaths`, `contents`, `repartConfig`

The output `system.build.image` is a compressed disk image with GPT partitioning.

### nixos-generators (Deprecated)

`nix-community/nixos-generators` was archived on 2026-01-30. Its functionality has been upstreamed into nixpkgs as `nixos-rebuild build-image`. Existing users should migrate:

```bash
# Old (nixos-generators)
nixos-generate -f amazon --flake .#myhost

# New (NixOS 25.05+)
nixos-rebuild build-image --image-variant amazon --flake .#myhost
```

### forge-metal Relevance

For bare-metal provisioning, the most relevant image types are:

**kexec** — the `kexec` image variant is central to tools like `nixos-anywhere`. It lets you take over a running Linux system (e.g., a Debian rescue image from Latitude.sh) and pivot into NixOS without rebooting to removable media. The `nixos-anywhere` workflow uses kexec + disko:

```bash
nix run github:nix-community/nixos-anywhere -- --flake .#myhost root@<ip>
```

**disko + `system.build.diskoImages`** — for creating pre-partitioned disk images locally, then `dd`-ing to bare metal. Especially useful for SD card / embedded deployments or pre-provisioned images.

---

## Topic 5: `nix flake check` — What Each Output Type Actually Does

This extends the coverage in [outputs-and-ci.md](outputs-and-ci.md) with deeper per-output-type behavior.

### Behavior Matrix

| Output type | Evaluated? | Built? | Validation performed |
|-------------|-----------|--------|---------------------|
| `packages.<sys>.<name>` | Yes | Yes (unless `--no-build`) | Must be a derivation; build must succeed |
| `devShells.<sys>.<name>` | Yes | No | Must be a derivation (does NOT enter the shell) |
| `checks.<sys>.<name>` | Yes | Yes (always, even with `--no-build`? No — `--no-build` skips all builds) | Must be a derivation; build must succeed |
| `apps.<sys>.<name>` | Yes | No | Must be `{ type = "app"; program = "..."; }`; `program` must be a store path string |
| `nixosConfigurations.<name>` | Yes (evaluates `.config.system.build.toplevel`) | No | Module system must not throw; `.toplevel` must be a derivation |
| `overlays.<name>` | Yes | No | Must be a function `final: prev: { ... }` |
| `nixosModules.<name>` | Yes | No | Must be a NixOS module value |
| `formatter.<sys>` | Yes | No | Must be a derivation |
| `hydraJobs.<attrs>` | Yes (recursively) | No (evaluation only) | Nested attrset of derivations (like `lib.recurseIntoAttrs`) |
| `legacyPackages.<sys>` | Yes (shallow) | No | Evaluated like `nix-env --query --available` (stops at non-attrsets) |
| `templates.<name>` | Yes | No | Must be `{ path = ...; description = "..."; }` |
| `bundlers.<sys>.<name>` | Yes | No | Must be a function |

**Key distinction**: `checks` derivations are the **only** outputs that `nix flake check` actually builds by default. `packages` derivations are also built. `devShells`, `apps`, `nixosConfigurations` are evaluated (type-checked) but not built.

### `packages` vs `checks` in `nix flake check`

```
nix flake check behavior:
  checks.* → ALWAYS BUILT (this is their purpose)
  packages.* → BUILT (unless --no-build)
  devShells.* → EVALUATED ONLY (type check, not built)
  nixosConfigurations.* → EVALUATED ONLY (.toplevel type-checked, not realized)
```

The difference matters for CI cost: if you put your test suite in `checks`, it always runs. If you put it in `packages`, it runs unless someone passes `--no-build`. Keep test/verification derivations in `checks`, distributable artifacts in `packages`.

### `nixosConfigurations` Validation Depth

`nix flake check` evaluates `nixosConfigurations.<name>.config.system.build.toplevel`. This means:
1. All NixOS module options are evaluated
2. All `config.*` values are computed
3. Module system assertions (`assertions = [ { assertion = ...; message = ...; } ]`) are checked — a failing assertion causes `nix flake check` to fail with the assertion message
4. The `.drv` file is computed (instantiation happens)
5. But the system is NOT built — no packages are downloaded/compiled

This catches:
- Module evaluation errors (type mismatches, unknown options)
- Failed assertions (e.g., conflicting services, required options not set)
- Missing option values

This does NOT catch:
- Build failures (a package that fails to compile)
- Runtime errors (a service that crashes when started)

To also build the NixOS system in CI, add it to `checks`:
```nix
checks.x86_64-linux.nixos-myhost = self.nixosConfigurations.myhost.config.system.build.toplevel;
```

### `apps` Validation

`nix flake check` validates the app schema strictly:
```nix
# Valid:
apps.x86_64-linux.deploy = {
  type = "app";                           # must be literal string "app"
  program = "${pkgs.ansible}/bin/ansible-playbook";  # must be store path
};

# Invalid (program is not a store path):
apps.x86_64-linux.bad = {
  type = "app";
  program = "/usr/bin/bash";   # absolute path outside store → fails check
};
```

### `--no-build` — Evaluation-Only Mode

```bash
nix flake check --no-build
```

Skips all builds. Useful in two situations:
1. Fast CI feedback on evaluation errors (type errors, missing options) before committing to a full build
2. Machines without Nix builders (pure eval check for documentation or validation jobs)

Note: even with `--no-build`, all derivations are still instantiated (`.drv` files computed). Only the realization step is skipped. This means IFD (Import From Derivation) still triggers builds since IFD requires realizing a derivation during evaluation. `--no-build` does not prevent IFD builds.

### `--keep-going` for CI

```bash
nix flake check --keep-going --print-build-logs
```

Without `--keep-going`, `nix flake check` stops at the first failing check. In CI, `--keep-going` is almost always what you want — it reports all failures in one run.

`--print-build-logs` (short: `-L`) shows build output inline rather than in a log file. Essential for debugging failing `checks` derivations in CI.

### `--all-systems` Caveat

```bash
nix flake check --all-systems
```

Checks outputs for all systems, not just the current. This means `packages.aarch64-linux.*` would be built on an x86_64 machine — requiring either QEMU emulation or a remote aarch64 builder. In most cases, `--all-systems` in CI requires `remote-build` configured or is undesirably slow.

The standard CI approach is to check only the target system and use separate CI runners per architecture.

### Writing Effective `checks` Derivations

Every check derivation must create `$out`. The content is ignored — only the exit code matters.

**Anti-pattern — forgetting `mkdir $out`:**
```nix
# WRONG: build succeeds but nix will error "output '$out' was not created"
checks.x86_64-linux.my-check = pkgs.runCommand "my-check" {} ''
  my-tool --check ./something
'';
```

**Correct patterns:**
```nix
# Pattern 1: mkdir $out after the check
checks.x86_64-linux.types = pkgs.runCommand "type-check" {
  nativeBuildInputs = [ pkgs.typescript ];
} ''
  tsc --noEmit ${./tsconfig.json}
  mkdir $out
'';

# Pattern 2: touch $out (when $out should be a file)
checks.x86_64-linux.license = pkgs.runCommand "license-check" {} ''
  grep -r "MIT License" ${./LICENSE}
  touch $out
'';

# Pattern 3: installPhase creates $out (for mkDerivation-style checks)
checks.x86_64-linux.go-tests = pkgs.stdenvNoCC.mkDerivation {
  name = "go-tests";
  src = ./.;
  nativeBuildInputs = [ pkgs.go ];
  doCheck = true;
  checkPhase = "go test ./...";
  installPhase = "touch $out";
};
```

**Using `pkgs.runCommandLocal` for fast checks:**
```nix
# Skips cache lookup — faster for sub-second checks
checks.x86_64-linux.nix-fmt = pkgs.runCommandLocal "nix-fmt-check" {
  nativeBuildInputs = [ pkgs.nixfmt-rfc-style ];
} ''
  nixfmt --check ${./flake.nix} ${./default.nix}
  touch $out
'';
```

### `nix flake check` Exit Codes

- `0` — all checks passed (all checks built successfully, all validations passed)
- `1` — at least one check failed

In CI (GitHub Actions, etc.):
```yaml
- name: Check flake
  run: nix flake check --keep-going --print-build-logs
```

---

## Factual Correction: `symlinkJoin` Is Not a `buildEnv` Wrapper

The document [advanced-topics.md](advanced-topics.md) previously stated that `symlinkJoin` is "a thin wrapper around `buildEnv`". This is incorrect as of current nixpkgs (the implementation was refactored).

**Current implementation** (`pkgs/build-support/trivial-builders/default.nix`):

`symlinkJoin` uses `lib.extendMkDerivation` with `stdenvNoCC.mkDerivation` and calls `lndir` directly:

```bash
# Actual build script in symlinkJoin:
mkdir -p $out
for i in $(cat $pathsPath); do
  lndir -silent $i $out
done
${postBuild}
```

It does NOT call `buildEnv` internally. The behavioral differences documented in `advanced-topics.md` (collision handling, `pathsToLink`, etc.) are still accurate — `buildEnv` has those features, `symlinkJoin` does not — but the implementation claim was wrong.

The `advanced-topics.md` entry has been updated to reflect this.
