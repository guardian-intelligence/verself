# nixpkgs.lib Standard Library, Hermetic Eval Builtins, and Flake Subcommands

Deep reference covering three underexplored areas: the `nixpkgs.lib` standard library (~300 functions), hermetic evaluation edge cases at the builtin level, and `nix flake` subcommands not covered elsewhere.

---

## Part 1: `nixpkgs.lib` Standard Library Deep Dive

`lib` is the attribute set exported from `nixpkgs/lib/default.nix`. It is available as `(import <nixpkgs> {}).lib` or, inside nixpkgs, as `pkgs.lib`. In flake code you typically access it as `nixpkgs.lib` from the flake input. The library is divided into sub-modules; the most important are covered below.

### 1.1 `lib.strings`

Source: `nixpkgs/lib/strings.nix`.

#### `escapeNixIdentifier`

```nix
escapeNixIdentifier :: String -> String
```

Quotes a string if it cannot be used directly as a Nix identifier. A valid unquoted identifier satisfies `[a-zA-Z_][a-zA-Z0-9_'-]*` and is not a Nix keyword. If the string fails either condition, the function wraps it with `escapeNixString` (which adds double-quotes and escapes internal special chars).

```nix
escapeNixIdentifier "hello"    # => "hello"       (valid, no quotes)
escapeNixIdentifier "0abc"     # => "\"0abc\""    (starts with digit)
escapeNixIdentifier "foo bar"  # => "\"foo bar\""  (contains space)
escapeNixIdentifier "if"       # => "\"if\""      (Nix keyword)
```

Practical use: generating `.nix` source from Nix itself (code-gen), writing `nix eval --expr` fragments with computed attribute names.

#### `replaceStrings`

```nix
replaceStrings :: [String] -> [String] -> String -> String
```

This is a thin wrapper around `builtins.replaceStrings`. **Critical misconception**: this function does NOT support regular expressions. It performs literal string substitution. The `from` list and `to` list must have the same length; each element of `from` is replaced by the corresponding element of `to`.

```nix
replaceStrings ["-" "_"] ["_" "-"] "foo-bar_baz"
# => "foo_bar-baz"   (literal, not regex)
```

Replacements are applied left-to-right and each position in the string is matched at most once (the scan advances past the replaced segment), so overlapping patterns are not an issue.

#### `splitString`

```nix
splitString :: String -> String -> [String]
```

Splits a string on a literal separator. Internally calls `builtins.split` (which takes a regex), then filters the result to keep only string elements (discarding the regex match group lists). Because `builtins.split` treats its first argument as a regex, `splitString` escapes the separator — but do not rely on this for complex patterns.

```nix
splitString "." "foo.bar.baz"    # => [ "foo" "bar" "baz" ]
splitString "/" "/usr/local/bin" # => [ "" "usr" "local" "bin" ]
```

Note the leading empty string when the input starts with the separator. Compare with `lib.strings.splitVersion` (which splits version strings on `.`) — `splitVersion` is a distinct function optimized for version components.

#### `concatMapStringsSep`

```nix
concatMapStringsSep :: String -> (a -> String) -> [a] -> String
```

Maps a function over a list, then joins the resulting strings with a separator. Equivalent to `concatStringsSep sep (map f list)`.

```nix
concatMapStringsSep ", " toString [ 1 2 3 ]  # => "1, 2, 3"
concatMapStringsSep "-" toUpper ["foo" "bar"] # => "FOO-BAR"
```

This is the idiomatic way to build separated-value strings from non-string lists in a single pass.

#### `toUpper` / `toLower`

```nix
toUpper :: String -> String
toLower :: String -> String
```

Both are implemented via `replaceStrings` with the 26 ASCII uppercase/lowercase character lists. **ASCII-only limitation**: Unicode characters outside the ASCII range (> 127) pass through unchanged. There is no Unicode case folding. Strings with UTF-8 content will have only their ASCII bytes case-converted; non-ASCII bytes are preserved verbatim.

```nix
toUpper "hello"   # => "HELLO"
toLower "HÉLLO"   # => "hÉllo"  (É is unchanged, ASCII h is lowercased)
```

#### `sanitizeDerivationName`

```nix
sanitizeDerivationName :: String -> String
```

Converts an arbitrary string into a valid Nix derivation name. The Nix store allows only `[a-zA-Z0-9+._?=-]` in store paths (with additional constraints). This function:

1. Strips leading dots (`.hidden` → `hidden`, since `..` is rejected)
2. Replaces invalid characters with `-`
3. Truncates to 207 characters (the maximum derivation name length)
4. Substitutes empty results with `"unknown"`

```nix
sanitizeDerivationName "../hello.bar # foo"  # => "-hello.bar--foo"
sanitizeDerivationName ""                     # => "unknown"
sanitizeDerivationName (lib.replicate 300 "x") # truncated to 207 chars
```

Used internally by `fetchurl`, `fetchGit`, and similar fetchers when generating store path names from URLs or filenames.

#### `nameValuePair`

```nix
nameValuePair :: String -> Any -> { name :: String; value :: Any }
```

Constructs a `{ name = ...; value = ...; }` attrset, which is the format expected by `builtins.listToAttrs`. This is the standard way to build an attrset from a list using `map` + `listToAttrs`:

```nix
builtins.listToAttrs (map (n: nameValuePair n (n + "_value")) [ "a" "b" ])
# => { a = "a_value"; b = "b_value"; }
```

Used extensively inside `mapAttrs'` (see §1.2) since that function expects its mapping function to return a `nameValuePair`.

#### `fixedWidthNumber`

```nix
fixedWidthNumber :: Int -> Int -> String
```

Zero-pads a number to the specified width. Implemented as `fixedWidthString width "0" (toString n)`.

```nix
fixedWidthNumber 5 15    # => "00015"
fixedWidthNumber 3 1000  # => "1000"  (no truncation if wider than width)
```

Use case: generating lexicographically-sortable filenames or IDs from counters in code generation.

#### `hasPrefix` / `hasSuffix` / `removePrefix` / `removeSuffix`

```nix
hasPrefix :: String -> String -> Bool
hasSuffix :: String -> String -> Bool
removePrefix :: String -> String -> String  # identity if not present
removeSuffix :: String -> String -> String  # identity if not present
```

Common string predicates. `removePrefix`/`removeSuffix` return the unchanged string if the prefix/suffix is absent (no error). Use `lib.strings.removePrefix` (not to be confused with `lib.lists.removePrefix`, which throws on mismatch).

---

### 1.2 `lib.attrsets`

Source: `nixpkgs/lib/attrsets.nix`.

#### `recursiveUpdate`

```nix
recursiveUpdate :: AttrSet -> AttrSet -> AttrSet
```

The recursive variant of the `//` operator. Merges two attrsets with the **right side winning** at every level. Recursion continues as long as both values at a given key are themselves attrsets. When either side is a non-attrset (string, int, list, derivation, etc.), recursion stops and the right-hand value replaces the left-hand value entirely.

```nix
recursiveUpdate
  { boot.loader.grub.enable = true; boot.loader.grub.device = "/dev/sda"; }
  { boot.loader.grub.device = ""; }
# => { boot.loader.grub.enable = true; boot.loader.grub.device = ""; }
# Right side wins on "device"; "enable" is preserved because it is absent on the right.

recursiveUpdate
  { a = { x = 1; }; }
  { a = "string"; }
# => { a = "string"; }
# Right side is not an attrset, so the entire "a" subtree is replaced.
```

**Key contrast with `//`**: `//` is always shallow — it replaces whole top-level values. `recursiveUpdate` goes deep but still replaces non-attrset leaves wholesale.

**Known renaming controversy**: GitHub issue #297264 proposed renaming `recursiveUpdate` to `mergeAttrsRecursive` for clarity, since the function merges (not just updates). The rename has not been merged as of 2026.

#### `mapAttrs'`

```nix
mapAttrs' :: (String -> a -> { name :: String; value :: b }) -> AttrSet -> AttrSet
```

Like `mapAttrs`, but the mapping function can also rename the key. The function must return a `nameValuePair`. This is the standard way to rename attrset keys:

```nix
mapAttrs' (name: value: nameValuePair ("foo_" + name) ("bar-" + value))
  { x = "a"; y = "b"; }
# => { foo_x = "bar-a"; foo_y = "bar-b"; }
```

Implemented internally as `listToAttrs (mapAttrsToList f set)`.

#### `filterAttrs`

```nix
filterAttrs :: (String -> a -> Bool) -> AttrSet -> AttrSet
```

Returns a new attrset containing only those attributes for which the predicate returns `true`. The predicate receives both the attribute name and value.

```nix
filterAttrs (n: v: n == "foo") { foo = 1; bar = 2; }  # => { foo = 1; }
filterAttrs (n: v: isInt v)    { a = 1; b = "x"; }     # => { a = 1; }
```

#### `genAttrs`

```nix
genAttrs :: [String] -> (String -> a) -> AttrSet
```

Generates an attrset from a list of names, where each value is produced by applying the function to the name.

```nix
genAttrs [ "x86_64-linux" "aarch64-linux" ] (system: pkgsFor system)
# The idiomatic way to build per-system package sets before flake-utils existed.
```

Note the argument order: names list first, function second. This is the reverse of `map`.

#### `attrByPath` / `setAttrByPath` / `getAttrFromPath`

```nix
attrByPath    :: [String] -> Any -> AttrSet -> Any  # returns default if missing
getAttrFromPath :: [String] -> AttrSet -> Any        # throws if missing
setAttrByPath :: [String] -> Any -> AttrSet          # creates nested structure
```

These three functions work with dotted-path navigation using a list of keys.

```nix
attrByPath ["a" "b"] 0 { a = { b = 42; }; }   # => 42
attrByPath ["z" "z"] 0 { a = { b = 42; }; }   # => 0 (default)

getAttrFromPath ["a" "b"] { a = { b = 42; }; } # => 42
getAttrFromPath ["z" "z"] { a = { b = 42; }; } # => throws: "cannot find attribute 'z.z' in ..."

setAttrByPath ["a" "b"] 42   # => { a = { b = 42; }; }
```

`setAttrByPath` creates from scratch — it does not merge with an existing set. To update nested paths, combine with `recursiveUpdate`:

```nix
recursiveUpdate existing (setAttrByPath ["a" "b"] 42)
```

#### `foldlAttrs`

```nix
foldlAttrs :: (acc -> String -> b -> acc) -> acc -> { [String] :: b } -> acc
```

Added in nixpkgs 23.05. Like `lib.lists.foldl'` but for attrsets. Processes attributes in **sorted key order** (alphabetical). The accumulator function receives `(acc, name, value)`.

```nix
foldlAttrs
  (acc: name: value: { sum = acc.sum + value; names = acc.names ++ [name]; })
  { sum = 0; names = []; }
  { foo = 1; bar = 10; }
# => { sum = 11; names = [ "bar" "foo" ]; }
# Note: "bar" comes before "foo" (alphabetical order)
```

Use case: building derived structures from a set when you need both the key and value, without converting to a list first.

#### `zipAttrsWith`

```nix
zipAttrsWith :: (String -> [a] -> b) -> [AttrSet] -> AttrSet
```

Merges a list of attrsets. For each key that appears in any input set, calls the combining function with the key name and a list of all values for that key across all input sets. Keys absent in some sets produce shorter lists (not null-padded).

```nix
zipAttrsWith (name: values: values)
  [ { a = "x"; } { a = "y"; b = "z"; } ]
# => { a = [ "x" "y" ]; b = [ "z" ]; }

# Deep merge with concatenation:
zipAttrsWith (name: values: lib.concatLists values)
  [ { pkgs = ["curl"]; } { pkgs = ["git"]; } ]
# => { pkgs = [ "curl" "git" ]; }
```

`zipAttrs` (no `With`) is the degenerate case that always collects values into lists: `zipAttrsWith (k: vs: vs)`.

---

### 1.3 `lib.lists`

Source: `nixpkgs/lib/lists.nix`.

#### `foldl'` (strict foldl)

```nix
foldl' :: (b -> a -> b) -> b -> [a] -> b
```

The strict left fold. Unlike `lib.lists.foldl` (which is lazy), `foldl'` forces evaluation of the accumulator at each step. This prevents stack overflows on large lists where the accumulator is a numeric or simple value. Use `foldl'` for any fold where the accumulator is not a thunk-dependent structure.

```nix
foldl' (acc: x: acc + x) 0 (lib.range 1 10000)  # works; foldl would stack-overflow
```

Note: `builtins.foldl'` also exists and is strict; `lib.lists.foldl'` was added when the builtin was not yet available on all Nix versions. As of Nix 2.4+, both are equivalent in strictness.

#### `imap0` / `imap1`

```nix
imap0 :: (Int -> a -> b) -> [a] -> [b]   # 0-indexed
imap1 :: (Int -> a -> b) -> [a] -> [b]   # 1-indexed
```

Map with index. The function receives the index first, then the element.

```nix
imap0 (i: v: "${v}-${toString i}") [ "a" "b" "c" ]  # => [ "a-0" "b-1" "c-2" ]
imap1 (i: v: "${v}-${toString i}") [ "a" "b" "c" ]  # => [ "a-1" "b-2" "c-3" ]
```

Implemented via `genList` + `elemAt` rather than `map` + index threading, which is why the index is the first argument.

#### `concatMap`

```nix
concatMap :: (a -> [b]) -> [a] -> [b]
```

Alias for `builtins.concatMap`. Maps a function that returns a list over each element, then flattens one level. The standard monad-style flat-map.

```nix
concatMap (x: [x x]) [ 1 2 3 ]  # => [ 1 1 2 2 3 3 ]
```

#### `flatten` vs `concatLists`

```nix
flatten     :: [a | [a | ...]] -> [a]   # recursive, arbitrary depth
concatLists :: [[a]] -> [a]              # one level only
```

`flatten` recursively unwraps nested lists to any depth. `concatLists` (alias: `builtins.concatLists`) only flattens one level. Use `concatLists` when you know your structure is exactly two levels deep (list of lists); use `flatten` for arbitrary depth.

```nix
flatten [ 1 [2 [3]] 4 ]         # => [ 1 2 3 4 ]
concatLists [ [1 2] [3 [4]] ]   # => [ 1 2 3 [4] ]  (inner [4] not unwrapped)
```

#### `subtractLists`

```nix
subtractLists :: [a] -> [a] -> [a]
```

Removes all elements in the first list from the second list. O(n*m) complexity. Preserves order of the second list.

```nix
subtractLists [ 3 2 ] [ 1 2 3 4 5 3 ]  # => [ 1 4 5 ]
```

Note argument order: the "subtract" set comes first, the "base" list second. This is the opposite of mathematical subtraction notation.

#### `unique`

```nix
unique :: [a] -> [a]
```

Removes duplicates using `foldl'` + `elem` membership test. O(n²) complexity — avoid on very large lists. Preserves order of first occurrence.

```nix
unique [ 3 2 3 4 ]  # => [ 3 2 4 ]
```

#### `naturalSort`

```nix
naturalSort :: [String] -> [String]
```

Sorts strings treating embedded numeric substrings as numbers rather than character sequences. Useful for filenames with version numbers.

```nix
naturalSort [ "disk11" "disk8" "disk100" "disk9" ]
# => [ "disk8" "disk9" "disk11" "disk100" ]
# Lexicographic sort would give: [ "disk100" "disk11" "disk8" "disk9" ]
```

#### `range`

```nix
range :: Int -> Int -> [Int]
```

Generates an inclusive integer range. Returns `[]` if `first > last`.

```nix
range 2 5   # => [ 2 3 4 5 ]
range 3 2   # => []
```

#### `take` / `drop` / `sublist`

```nix
take    :: Int -> [a] -> [a]            # first n elements
drop    :: Int -> [a] -> [a]            # all but first n elements
sublist :: Int -> Int -> [a] -> [a]     # n elements starting at index
```

```nix
take 2 [ "a" "b" "c" "d" ]       # => [ "a" "b" ]
drop 2 [ "a" "b" "c" "d" ]       # => [ "c" "d" ]
sublist 1 3 [ "a" "b" "c" "d" ]  # => [ "b" "c" "d" ]
```

Note: `splitAt` (Haskell-style `(take n xs, drop n xs)`) is **not** in `lib.lists`. Use `take` + `drop` separately, or `partition`-based equivalents.

#### `toposort`

```nix
toposort :: (a -> a -> Bool) -> [a] -> { result :: [a] } | { cycle :: [a]; loops :: [a] }
```

Topological sort using depth-first search. The comparator `before a b` should return `true` if `a` must come before `b`. Returns `{ result }` on success or `{ cycle, loops }` on detection of a cycle. O(N²).

Used internally by the NixOS module system to order modules that declare `after`/`before` ordering constraints.

```nix
toposort (a: b: a < b) [ 3 2 1 ]  # => { result = [ 1 2 3 ]; }
```

#### Notable missing functions

The following Haskell-inspired functions are **not** in `lib.lists`:
- `splitAt` — use `take n` and `drop n` separately
- `chunksOf` — not present; implement with `genList` + `sublist`
- `transpose` — not present in the public API

---

### 1.4 `lib.trivial`

Source: `nixpkgs/lib/trivial.nix`.

#### `pipe`

```nix
pipe :: a -> [(a -> b) (b -> c) ... (y -> z)] -> z
```

Left-to-right function composition. Implemented as `builtins.foldl' (x: f: f x) value functions`. Unlike Haskell's `$` or `.`, `pipe` takes the initial value first, then the list of functions — reading order matches data flow.

```nix
pipe 2 [
  (x: x * 3)
  (x: x + 1)
  toString
]
# => "7"   (2 * 3 = 6, 6 + 1 = 7, toString 7 = "7")
```

The idiomatic alternative to deeply nested function calls: `toString (f (g (h x)))` → `pipe x [h g f]`.

#### `flip`

```nix
flip :: (a -> b -> c) -> (b -> a -> c)
```

Swaps the first two arguments of a binary function.

```nix
flip builtins.sub 3 10  # => 10 - 3 = 7  (instead of 3 - 10 = -7)
```

#### `const`

```nix
const :: a -> b -> a
```

Returns its first argument, ignoring the second. Useful for producing constant-valued functions in `map`/`genAttrs` contexts:

```nix
map (const true) [ 1 2 3 ]  # => [ true true true ]
```

#### `id`

```nix
id :: a -> a
```

Identity function. Used as a no-op in `pipe` chains or as a default "no transform" value.

#### `warn` / `warnIf` / `traceIf`

```nix
warn   :: String -> a -> a        # always prints warning, returns second arg
warnIf :: Bool -> String -> a -> a # prints warning only if condition is true
traceIf :: Bool -> String -> a -> a # like trace but conditional
```

`warn` is implemented via `builtins.warn` (added in Nix 2.17) with a fallback to `builtins.trace`. Setting the environment variable `NIX_ABORT_ON_WARN=1` converts warnings into hard evaluation errors — useful in CI to catch deprecated usage.

`traceIf` uses `builtins.trace` (always prints to stderr during evaluation) but gated on a boolean. The difference from `warnIf`: `trace` goes to stderr unconditionally; `warn` may be suppressed or treated differently by the Nix evaluator (e.g., deduplicated).

```nix
warnIf (lib.versionOlder lib.version "23.05")
  "This expression requires nixpkgs ≥ 23.05"
  someValue
```

#### `importJSON` / `importTOML`

```nix
importJSON :: Path -> Any    # path -> builtins.fromJSON (builtins.readFile path)
importTOML :: Path -> Any    # path -> builtins.fromTOML (builtins.readFile path)
```

These are one-liners that compose the read + parse builtins. They require that the path is available at evaluation time (normal source file constraint). `importTOML` depends on `builtins.fromTOML`, which was added in Nix 2.6.

```nix
# Read package.json to get version
(lib.importJSON ./package.json).version

# Read config.toml
(lib.importTOML ./config.toml).database.host
```

#### `boolToString`

```nix
boolToString :: Bool -> String
```

Returns `"true"` or `"false"` (lowercase string literals). This is distinct from `lib.generators.mkValueStringDefault` which may format booleans differently for specific config formats.

#### `defaultTo`

```nix
defaultTo :: a -> a | null -> a
```

Null coalescing: returns the second argument if it is not `null`, otherwise returns the first (default) argument.

```nix
defaultTo "localhost" config.host   # uses "localhost" if config.host is null
defaultTo "localhost" null          # => "localhost"
defaultTo "localhost" "prod.host"   # => "prod.host"
```

---

### 1.5 `lib.filesystem` (added nixpkgs ~21.05 via PR #101096)

Source: `nixpkgs/lib/filesystem.nix`.

The `lib.filesystem` module was added incrementally; `listFilesRecursive` predates the named module. Functions here wrap `builtins.readDir` and `builtins.readFileType` with more ergonomic interfaces.

#### `pathType`

```nix
pathType :: Path -> String   # "regular" | "directory" | "symlink" | "unknown"
```

A direct alias for `builtins.readFileType`. Returns a string describing the filesystem entry type. Throws if the path does not exist.

```nix
lib.filesystem.pathType /.               # => "directory"
lib.filesystem.pathType /etc/hosts       # => "regular"
lib.filesystem.pathType /dev/null        # => "unknown"
```

#### `pathIsDirectory` / `pathIsRegularFile`

```nix
pathIsDirectory   :: Path -> Bool
pathIsRegularFile :: Path -> Bool
```

Both check existence first (via `builtins.pathExists`), then compare `pathType`. Return `false` for absent paths rather than throwing.

```nix
lib.filesystem.pathIsDirectory /etc          # => true
lib.filesystem.pathIsDirectory /etc/hosts    # => false (it's a file)
lib.filesystem.pathIsDirectory /nonexistent  # => false (not an error)
```

#### `listFilesRecursive`

```nix
listFilesRecursive :: Path -> [Path]
```

Recursively traverses a directory using `builtins.readDir`, building nested lists then flattening at the end. Returns a flat list of all regular file paths (symlinks and special files included as paths; directories are not included in the output list).

```nix
lib.filesystem.listFilesRecursive ./src
# => [ /path/to/src/a.nix /path/to/src/sub/b.nix ... ]
```

**Contrast with `builtins.readDir`**: `readDir` returns only one level as an attrset of `name -> type` pairs. `listFilesRecursive` recurses and returns absolute paths, ready for use in `map` or `lib.fileset` operations.

---

### 1.6 `lib.path` (added nixpkgs 23.11)

Source: `nixpkgs/lib/path/default.nix`. Tracked via PR #200718.

The `lib.path` module provides type-safe path arithmetic, replacing the common (but fragile) pattern of `toString path + "/" + name`. This module was formalized in nixpkgs 23.11; `splitRoot` and `subpath.components` were part of the 23.11 additions.

#### `lib.path.append`

```nix
append :: Path -> String -> Path
```

Safely appends a subpath string to a path value. Validates both arguments.

```nix
lib.path.append /foo "bar/baz"        # => /foo/bar/baz
lib.path.append /foo "./bar//baz/./"  # => /foo/bar/baz  (normalizes)
lib.path.append /. "foo/bar"          # => /foo/bar
```

Errors on invalid inputs:
- First argument is not a path type: `"The first argument is of type ${type}, but a path was expected"`
- Second argument fails subpath validation: `"Second argument is not a valid subpath string: ${reason}"`

**Why not `path + ("/" + string)`?** The string concatenation approach silently coerces the path to a store path string, which then gets added to the Nix store as a derivation dependency. `lib.path.append` keeps the result as a path value, avoiding unnecessary store copies.

#### `lib.path.subpath`

The `subpath` attribute is itself an attrset of helpers:

**`subpath.isValid :: String -> Bool`**

Returns `true` if the string is a valid subpath: non-empty, does not start with `/`, and contains no `..` components.

```nix
lib.path.subpath.isValid "foo/bar"   # => true
lib.path.subpath.isValid ""          # => false
lib.path.subpath.isValid "/abs"      # => false
lib.path.subpath.isValid "../escape" # => false
```

The `..` restriction prevents directory traversal when appending user-provided paths.

**`subpath.normalise :: String -> String`**

Canonicalizes a valid subpath: collapses `//`, removes `.` components, strips trailing `/`, adds `./` prefix.

```nix
lib.path.subpath.normalise "foo//bar"    # => "./foo/bar"
lib.path.subpath.normalise "foo/./bar"   # => "./foo/bar"
lib.path.subpath.normalise "."           # => "./."
```

Idempotent: normalising twice gives the same result.

**`subpath.join :: [String] -> String`**

Concatenates multiple valid subpaths with `/` and normalizes the result. Empty list returns `"./"`.

```nix
lib.path.subpath.join [ "foo" "bar/baz" ]  # => "./foo/bar/baz"
lib.path.subpath.join []                    # => "./"
```

#### `lib.path.splitRoot`

```nix
splitRoot :: Path -> { root :: Path; subpath :: String }
```

Decomposes a path into its filesystem root and relative subpath.

```nix
lib.path.splitRoot /foo/bar   # => { root = /.; subpath = "./foo/bar"; }
lib.path.splitRoot /.         # => { root = /.; subpath = "./."; }
```

Nix automatically normalizes `..` in path literals at parse time, so `/foo/../bar` is equivalent to `/bar` before `splitRoot` even sees it.

**Practical use**: comparing two paths to determine if one is a subpath of another without string manipulation.

---

## Part 2: Hermetic Evaluation — Builtin Edge Cases

Nix flakes evaluate in **pure mode** by default. The `--impure` flag relaxes this for the evaluation phase only; build-time sandboxing is a separate mechanism.

### 2.1 `builtins.currentSystem`

**Pure mode**: throws `error: attribute 'currentSystem' missing` — the builtin is simply absent from the evaluation context.

**Impure mode** (`--impure` or `nix.conf` setting `pure-eval = false`): returns the string from the Nix configuration (e.g., `"x86_64-linux"`), which defaults to the host system but can be overridden with `--system aarch64-linux`.

**Historical controversy**: Before flakes, Nix code routinely used `builtins.currentSystem` to specialize derivations for the local machine. Flakes forced a choice: either ban it (breaking existing code) or require `--impure` (which weakens reproducibility guarantees). The community consensus was to ban it in pure mode, pushing packages toward the `forEachSystem`/`perSystem` pattern where the system is an explicit parameter rather than an implicit ambient value.

**Workaround**: `nix build --system aarch64-linux` passes the target system explicitly without making the entire evaluation impure.

### 2.2 `builtins.currentTime`

**Pure mode**: `builtins.currentTime` is **not** a banned builtin — it is **absent** from the eval context in pure mode, producing an eval error if accessed. It does NOT return `0` as a sentinel (this is a common misconception). In impure mode it returns the Unix epoch integer at first evaluation; subsequent references within the same evaluation reuse the cached value.

```nix
# In impure mode:
builtins.currentTime   # => e.g. 1748000000  (integer, seconds since epoch)

# In pure mode:
builtins.currentTime   # => error: attribute 'currentTime' missing
```

The "returns 0" claim in some blog posts is incorrect and refers to an older Nix behavior that was removed. As of Nix 2.4+, the builtin is simply absent.

### 2.3 `builtins.storePath`

```nix
storePath :: String -> Path
```

**Pure mode**: absent, throws.

**Impure mode**: Takes a store path string (e.g., `/nix/store/abc123-foo-1.0`) and returns it as a Nix path value, creating a **build-time dependency** on that exact store path without copying or rebuilding it. The specified path must already exist in the store.

```nix
builtins.storePath "/nix/store/abc123-foo-1.0"
# Creates a derivation reference; the path must exist at build time
```

**Security implication**: `storePath` bypasses the normal input-addressed derivation chain. You can reference an arbitrary store path that was not built by any derivation in the current closure — for example, a manually-installed or remotely-substituted path. This breaks reproducibility: the same expression may produce different results depending on what happens to be in the local store.

### 2.4 `builtins.path`

```nix
builtins.path :: {
  path    :: Path;
  name    :: String?;     # defaults to basename of path
  filter  :: (String -> String -> Bool)?;
  recursive :: Bool?;     # default true
  sha256  :: String?;     # if provided, makes this a FOD
} -> Path
```

This is the enhanced path type constructor. Key behaviors:

- Without `sha256`: behaves like a regular path reference — copies the path into the store (NAR-hashed) at evaluation time. Available in both pure and impure mode for local paths.
- With `sha256`: becomes a fixed-output derivation for the copy operation. This is what **enables `builtins.path` in pure eval**: when a hash is provided, the store path is content-addressed and can be validated without any filesystem access.

The `filter` function receives `(path, type)` and returns `true` to include the file. This is how you express "copy only `.hs` files" patterns for Haskell projects, etc.

```nix
# Copy only .go files:
builtins.path {
  path = ./.;
  name = "go-sources";
  filter = path: type: lib.hasSuffix ".go" path || type == "directory";
}
```

When `sha256` is set, the result is content-verified — any change to the source that produces a different NAR hash will fail the build until `sha256` is updated, just like other FODs.

### 2.5 `--impure` Flag Scope

The `--impure` flag (or `pure-eval = false` in `nix.conf`) affects **evaluation time only**, not build time. Specifically, it makes available:

| Builtin | Available in `--impure` | Notes |
|---------|------------------------|-------|
| `builtins.currentSystem` | Yes | Returns host system string |
| `builtins.currentTime` | Yes | Unix epoch integer at eval start |
| `builtins.getEnv` | Yes | Returns actual env var value |
| `builtins.storePath` | Yes | References arbitrary store paths |
| `builtins.readFile` | Yes (of local paths) | Already available in pure mode for store paths |
| `builtins.fetchGit` (without `rev`) | Partial | Still requires network; `--refresh` needed |

`builtins.fetchGit` without an explicit `rev` is **not** fully enabled by `--impure` alone — it needs `--refresh` or explicit `ref` + no lock to actually re-fetch. Without `--refresh`, a previously fetched rev from the eval cache may be returned.

### 2.6 `builtins.getEnv`

```nix
getEnv :: String -> String
```

**Pure mode**: Returns `""` for all inputs — not an error, a silent empty string. This is the most insidious of the pure-mode behaviors because code that does:

```nix
config.apiKey = builtins.getEnv "API_KEY";
```

...will silently get an empty string in pure mode without any warning. The code appears to work but produces wrong results. This is why `builtins.getEnv` carries a strong documentation warning: "can introduce all sorts of nasty environment dependencies."

**Impure mode**: Returns the actual environment variable value, or `""` if unset.

To detect misconfiguration, add an assertion:

```nix
let
  key = builtins.getEnv "API_KEY";
in
  assert key != "" || abort "API_KEY must be set; use --impure";
  key
```

### 2.7 `allowed-uris` and `restrict-eval`

**`restrict-eval`** (`nix.conf` option `restrict-eval = true`): The Nix evaluator refuses to access any files outside the Nix search path or URIs outside `allowed-uris`. This is the mode Hydra uses. Under `restrict-eval`:

- `builtins.fetchurl` can only fetch URIs matching prefixes in `allowed-uris`
- `builtins.fetchGit` is similarly restricted
- File access is limited to store paths and explicitly allowed paths

**`allowed-uris`**: A list of URI prefixes. Matching is by prefix string, not domain. Example:

```
allowed-uris = https://github.com/NixOS https://cache.nixos.org
```

This allows `https://github.com/NixOS/nixpkgs/...` but NOT `https://github.com/Evil/repo/...`. For flakes, the typical setting is:

```
allowed-uris = github: git+https://github.com/ git+ssh://github.com/
```

**Interaction with flakes under Hydra**: Because Hydra uses `restrict-eval`, flake inputs must be fetched as store paths before evaluation starts. The Hydra jobset configuration provides input paths; the evaluator sees them as store paths, bypassing the URI restriction. This is why Hydra flake support required special handling in the Nix evaluator.

**`nixpkgs.config.allowedUris`**: This is a **completely different mechanism** — it controls the `allowedUris` in nixpkgs's own fetcher wrappers, not the Nix daemon's `allowed-uris`. They do not interact.

### 2.8 `accept-flake-config` and `nixConfig.sandbox = false`

**`nixConfig` in `flake.nix`** can set `nix.conf` values that affect how the flake is built. The `accept-flake-config` setting controls whether these are applied automatically or require user confirmation.

When a flake sets:

```nix
nixConfig = {
  sandbox = false;
  extra-sandbox-paths = [ "/etc/ssl" ];
};
```

And the user has `accept-flake-config = true` in their `nix.conf`, the build sandbox is **disabled** without any prompt. This is a significant security issue: `sandbox = false` allows derivations to access the network, read the host filesystem, and write anywhere the Nix daemon can write. GitHub issue #9649 documented that this is equivalent to arbitrary code execution as the Nix daemon user (root on multi-user installs).

**Security recommendation**: Keep `accept-flake-config = false` (the default). The interactive prompt that appears when the setting is absent gives users a chance to audit the nixConfig before accepting. See the existing `docker-vms-scripts-fmt-security.md` for the CVE-2024-38531 and CVE-2024-51481 coverage which relates to this class of vulnerabilities.

### 2.9 Impure Derivations (`__impure = true`)

An impure derivation is a derivation with `__impure = true` set as an attribute. This is an experimental Nix feature (requires `--extra-experimental-features impure-derivations`) that allows a derivation to access the network at build time **without** specifying a fixed output hash.

```nix
derivation {
  name = "fetch-something";
  builder = ./fetch.sh;
  __impure = true;  # allows network access; output is not content-addressed
  system = builtins.currentSystem;
}
```

Key properties:
- The output is **not content-addressed** and is **not substituted from binary caches** — it is always rebuilt
- Requires the `impure-derivations` experimental feature enabled on the Nix daemon
- The `--allow-unsafe-native-code-during-evaluation` flag is a separate feature enabling native plugins in the evaluator; it is not required for `__impure` derivations

Use case: remote state fetchers, dynamic content that must be fresh on every build, or bootstrapping builds that pull from internal registries. This feature is rarely used in practice and remains experimental. Most projects use fixed-output derivations (FODs) instead.

---

## Part 3: `nix flake` Subcommands

### 3.1 `nix flake init` and `nix flake new` — Template System

#### Template declaration in `flake.nix`

A flake declares templates via the `templates` output:

```nix
{
  outputs = { self, ... }: {
    templates = {
      go-service = {
        path = ./templates/go-service;       # directory to copy
        description = "A minimal Go HTTP service with Nix devshell";
        welcomeText = ''
          # Go Service Template

          Run `nix develop` to enter the dev shell.
          Edit `main.go` to start coding.
        '';
      };

      default = self.templates.go-service;   # used when no template name given
    };
  };
}
```

**`path`**: The directory whose contents are copied into the user's directory. The copy is recursive; files that already exist at the destination are **not overwritten** (non-destructive).

**`description`**: One-line CommonMark text shown by `nix flake show` and `nix flake init --list`.

**`welcomeText`**: Markdown block displayed to the user after successful initialization. Used for "next steps" instructions. Only shown on `init`, not on subsequent operations.

**`templates.default`**: When a user runs `nix flake init` (no `-t`), Nix looks for `templates.default` in the `templates#` flake (the official NixOS/templates repository). When referencing your own flake: `nix flake init -t .#` uses `.#templates.default`.

#### `nix flake init` vs `nix flake new`

| Command | Behavior |
|---------|----------|
| `nix flake init -t <template>` | Copies template files into the **current directory** |
| `nix flake new -t <template> ./path` | Creates **a new directory** at `./path` and copies template files into it |

`nix flake init` refuses to overwrite existing files. `nix flake new` similarly; it errors if the target directory already exists.

```bash
# Initialize current directory:
nix flake init -t github:NixOS/templates#rust

# Create new project directory:
nix flake new -t github:NixOS/templates#go-hello ./my-service
```

The official template repository at `github:NixOS/templates` provides: `bash-hello`, `c-hello`, `dotnet`, `empty`, `full`, `go-hello`, `haskell-hello`, `python`, `ruby`, `rust`, `rust-web-server`, `simple-container`, `trivial`, `typescript`, and others.

### 3.2 `nix flake update` — Selective Input Updates

**Modern syntax** (Nix 2.19+):

```bash
nix flake update                          # update all inputs
nix flake update nixpkgs                  # update only nixpkgs input
nix flake update nixpkgs home-manager     # update two inputs
```

**Deprecated `--update-input` flag** (removed/deprecated in Nix 2.19): The old syntax was `nix flake update --update-input nixpkgs`. The flag is now deprecated in favor of positional arguments. Both forms modify `flake.lock`.

**Transitive `follows` after an update**: `follows` is a static relationship declared in `flake.nix` inputs. When you update an input, Nix resolves the new lock entry for that input and then re-resolves all transitive dependencies of that input — but `follows` overrides are applied before writing the new lock. If `home-manager.inputs.nixpkgs.follows = "nixpkgs"`, updating `nixpkgs` will cause `home-manager`'s nixpkgs entry in the lock to point to the new nixpkgs revision automatically. The `follows` relationship survives updates.

### 3.3 `nix flake clone`

```bash
nix flake clone <flake-url> --dest <directory>
```

Performs a Git (or Mercurial) clone of the repository containing the flake. This gives you a working checkout of the flake's source, unlike `nix flake prefetch` which only copies to the Nix store.

```bash
nix flake clone dwarffs --dest ./dwarffs-src
cd dwarffs-src
nix build
```

Use case: inspect or modify upstream flake source, contribute patches, or use a flake as a starting point. The cloned directory is a full VCS checkout, not a read-only store path.

### 3.4 `nix flake show --json`

```bash
nix flake show
nix flake show --json
```

**Without `--json`**: Renders a tree of all flake outputs, organized by output type and system. Derivations show their name and description.

**With `--json`**: Outputs a JSON object with the same structure. Each output attribute contains a `type` field and (for packages/devShells) a `name` and `description`. Example fragment:

```json
{
  "packages": {
    "x86_64-linux": {
      "default": {
        "type": "derivation",
        "name": "bmci-0.1.0",
        "description": "Bare metal CI orchestrator"
      }
    }
  },
  "devShells": {
    "x86_64-linux": {
      "default": {
        "type": "derivation",
        "name": "nix-shell"
      }
    }
  }
}
```

**How `nix flake show` differs from `nix flake check`**:

| `nix flake show` | `nix flake check` |
|------------------|-------------------|
| Displays output structure | Validates outputs actually build/pass |
| Evaluates lazily (fast) | Forces derivations to build |
| No side effects | Runs NixOS VM tests, `checks.*` derivations |
| Machine-readable with `--json` | Exit code indicates pass/fail |
| No build | Requires working build environment |

`nix flake show` is purely informational and evaluates just enough to discover output names. `nix flake check` runs `nix build` on all `checks.*` outputs and validates that `packages`, `devShells`, etc. are valid derivations.

### 3.5 `nix flake prefetch`

```bash
nix flake prefetch <flake-url>
nix flake prefetch github:NixOS/nixpkgs/nixos-unstable
nix flake prefetch --json github:NixOS/nixpkgs/nixos-unstable
```

Downloads the flake's source tree into the Nix store **without evaluating the flake**. The target does not need to be an actual flake — it can be any fetchable source (tarball, git repo, etc.).

**Output** (plain):
```
path is '/nix/store/abc123-source'
```

**Output with `--json`**:
```json
{
  "hash": "sha256-...",
  "storePath": "/nix/store/abc123-source"
}
```

Use cases:
- Pre-warm the store with a large nixpkgs before `nix build` (avoids evaluation blocking on slow network)
- Verify the NAR hash of a flake input matches expectations
- Debugging fetch issues without triggering full evaluation
- Use with `--out-link` / `-o` to create a gcroot preventing garbage collection of the fetched source

The `--json` output's `hash` field is a SRI hash (`sha256-<base64>`) corresponding to the NAR hash of the fetched tree — the same format used in `flake.lock` and `narHash` fields.

### 3.6 `nix registry` — Full Reference

#### Registry types and priority

Nix maintains four registry layers, from lowest to highest precedence:

1. **Global registry**: Downloaded from the URL in the `flake-registry` nix.conf setting (default: `https://raw.githubusercontent.com/NixOS/flake-registry/master/flake-registry.json`). Cached locally and refreshed based on `tarball-ttl`. The global registry defines well-known flake aliases like `nixpkgs`, `home-manager`, `nix-darwin`.

2. **System registry**: `/etc/nix/registry.json`. Shared across all users on the machine. On NixOS, managed via `nix.registry` in `configuration.nix`.

3. **User registry**: `~/.config/nix/registry.json`. Modifiable by the current user via `nix registry add/remove/pin`.

4. **Command-line overrides**: `--override-flake <from> <to>`. Highest precedence, session-scoped.

#### Critical scoping rules (changed in Nix 2.26)

Prior to Nix 2.26, all four registry layers were consulted when resolving indirect references in both `flake.nix` lock file generation and CLI commands. Starting with **Nix 2.26** (shipped December 2024, PR #12019):

- **Lock file generation** now only uses the **global registry** and `--override-flake`. System and user registries are ignored during `nix flake lock` / `nix flake update`.
- CLI commands (`nix run nixpkgs#hello`, `nix shell nixpkgs`) still consult all four layers.
- A subsequent issue (#13104, 2025) reported that `nix run`/`nix shell` may also be ignoring user/system registries in some versions — this behavior is in flux.

The motivation: preventing local registry overrides (e.g., `nixpkgs` → `/home/user/nixpkgs`) from leaking into committed `flake.lock` files and affecting other users' builds.

The pre-existing documentation note still applies: "The system and user registries are not used to resolve flake references in `flake.nix`." This was true even before 2.26, but now it extends to lock file generation on the CLI as well.

#### Registry JSON format

```json
{
  "version": 2,
  "flakes": [
    {
      "from": { "type": "indirect", "id": "nixpkgs" },
      "to":   { "type": "github", "owner": "NixOS", "repo": "nixpkgs" }
    }
  ]
}
```

#### Matching and unification

A registry entry matches an input `R` if the `from` reference's attributes are a subset of `R`'s attributes. Matching is prefix-like:

- `nixpkgs` matches `nixpkgs` (exact) and `nixpkgs/nixos-24.05` (has additional `ref`)
- `nixpkgs/nixos-24.05` does NOT match `nixpkgs` (missing `ref`)

After matching, the `to` reference is **unified** with `R` by applying any `ref` and `rev` from `R` onto `to`. Example:

```
Registry: nixpkgs -> github:NixOS/nixpkgs
Input:    nixpkgs/nixos-24.05
Unified:  github:NixOS/nixpkgs/nixos-24.05
```

#### `nix registry add`

```bash
nix registry add <from> <to>
nix registry add my-nixpkgs /home/user/nixpkgs   # point to local checkout
nix registry add nixpkgs github:NixOS/nixpkgs/nixos-24.05  # pin to branch
```

Writes to the user registry. Overwrites existing entry for the same `from` id.

#### `nix registry remove`

```bash
nix registry remove nixpkgs
```

Removes the entry from the user registry. Does not affect system or global registries.

#### `nix registry list`

```bash
nix registry list
```

Displays all entries from all registries, with their source (global/system/user). Useful for debugging resolution.

#### `nix registry pin`

```bash
nix registry pin nixpkgs
nix registry pin nixpkgs github:NixOS/nixpkgs/abc123rev
```

Resolves the current `nixpkgs` to its exact revision and writes a pinned entry to the user registry. After pinning, all CLI uses of `nixpkgs` will use the pinned commit, regardless of upstream changes.

```bash
# Pin nixpkgs to the revision used in your current project:
nix registry pin nixpkgs \
  "github:NixOS/nixpkgs/$(jq -r '.nodes.nixpkgs.locked.rev' flake.lock)"
```

Note: `nix registry pin` writes to the **user** registry, which (since Nix 2.26) does not affect `flake.lock` generation. It only affects CLI commands.

#### The `flake:nixpkgs` alias resolution chain

When you run `nix shell nixpkgs#hello`:

1. `nixpkgs` is an indirect flake reference (type `indirect`, id `nixpkgs`)
2. Nix searches registries in priority order (user > system > global)
3. Global registry entry: `nixpkgs` → `github:NixOS/nixpkgs`
4. No `ref` or `rev` in the input → uses the default branch of the repo
5. `github:NixOS/nixpkgs` is fetched, evaluated, and `packages.${system}.hello` is resolved

The prefix `flake:` is an explicit way to reference a registry entry: `flake:nixpkgs` is equivalent to `nixpkgs` as an indirect reference. The `flake:` protocol type is how registry lookups are expressed internally.

---

## Summary Table

| Topic | Key Fact |
|-------|----------|
| `lib.strings.replaceStrings` | Literal substitution only — no regex |
| `lib.strings.toUpper`/`toLower` | ASCII-only; Unicode bytes pass through unchanged |
| `lib.strings.sanitizeDerivationName` | Truncates at 207 chars; empty result becomes `"unknown"` |
| `lib.strings.nameValuePair` | In `lib.attrsets`, not `lib.strings`; feeds `listToAttrs` |
| `lib.attrsets.recursiveUpdate` | Right-wins, stops recursing at non-attrset leaf |
| `lib.attrsets.foldlAttrs` | Added nixpkgs 23.05; processes keys in alphabetical order |
| `lib.attrsets.zipAttrsWith` | Missing keys produce shorter lists, not null-padded |
| `lib.lists.foldl'` | Strict; prevents stack overflow on large numeric folds |
| `lib.lists.flatten` vs `concatLists` | `flatten` is recursive; `concatLists` is one level only |
| `lib.lists.splitAt`/`chunksOf` | Do NOT exist in `lib.lists`; use `take`+`drop` or `sublist` |
| `lib.trivial.pipe` | Left-to-right composition; value first, function list second |
| `lib.trivial.importTOML` | Requires Nix 2.6+ (`builtins.fromTOML`) |
| `lib.filesystem` | `listFilesRecursive` uses `builtins.readDir` recursively |
| `lib.path` | Added nixpkgs 23.11; `append` avoids store-path coercion |
| `builtins.currentTime` | NOT `0` in pure mode — it is **absent** (throws) |
| `builtins.getEnv` | Returns `""` silently in pure mode (no error) |
| `builtins.storePath` | References arbitrary existing store path; breaks reproducibility |
| `builtins.path` with `sha256` | Enables use in pure eval; becomes a FOD |
| `restrict-eval` + `allowed-uris` | Prefix matching; Hydra uses this; separate from `nixpkgs.config.allowedUris` |
| `__impure = true` derivation | Always rebuilt; requires `impure-derivations` experimental feature |
| `nix flake init` vs `new` | `init`: current dir; `new`: creates new dir |
| `templates.welcomeText` | Shown after init; CommonMark; good for next-steps docs |
| `nix flake show --json` | Structural metadata only; no builds |
| `nix flake prefetch` | Fetches to store without evaluation; returns NAR hash |
| `nix registry` — Nix 2.26 change | System/user registries no longer affect lock file generation |
| `nix registry pin` | Writes to user registry; does not affect `flake.lock` |
| `accept-flake-config = true` | Equivalent to giving every flake root access to the Nix daemon |
