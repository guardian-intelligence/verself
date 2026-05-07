import { promises as fs } from "node:fs";
import { isBuiltin } from "node:module";
import path from "node:path";
import type { Nitro } from "nitro/types";

// Rolldown's `__commonJSMin` wrapper bodies sometimes retain a literal
// `__require("<external>")` call instead of switching to the bundled
// chunk-local `require_<X>()` symbol. The deployed Nitro server has no
// node_modules next to .output/server/, so the runtime require throws
// MODULE_NOT_FOUND, React aborts the surrounding Suspense boundary, and
// the browser falls back to client rendering. Tracker: nitrojs/nitro#4171.
//
// Wired as a `compiled` hook on the Nitro Vite plugin, this walks every
// emitted chunk under nitro.options.output.serverDir and rewrites each
// `__require("<spec>")` to `<symbol>()` when `<symbol>` is already imported
// into the chunk. Node built-in specifiers are left alone — the runtime
// resolves them natively.

const SCAN_EXTS = [".mjs", ".js"];
const REQUIRE_IMPORT_RE = /import\s*\{([^}]+)\}\s*from\s*["'][^"']+["']/g;
const ALIAS_RE = /(?:[\w$]+\s+as\s+)?(require_[A-Za-z0-9_$]+)/g;
const REQUIRE_CALL_RE = /\b__require\s*\(\s*["']([^"']+)["']\s*\)/g;

export async function rewriteCjsRequireOnCompiled(nitro: Nitro): Promise<void> {
  const serverDir = nitro.options.output.serverDir;
  const files: string[] = [];
  await walk(serverDir, files);

  let replaced = 0;
  let touchedFiles = 0;
  const missed = new Map<string, number>();

  for (const file of files) {
    const original = await fs.readFile(file, "utf8");
    if (!original.includes("__require(")) {
      continue;
    }
    const result = rewriteChunk(original);
    if (result.replacements > 0) {
      await fs.writeFile(file, result.source);
      replaced += result.replacements;
      touchedFiles += 1;
    }
    for (const spec of result.missed) {
      missed.set(spec, (missed.get(spec) ?? 0) + 1);
    }
  }

  if (missed.size > 0) {
    const summary = [...missed.entries()].map(([spec, n]) => `${spec}×${n}`).join(", ");
    console.warn(
      `[verself:rewrite-cjs-require] ${missed.size} external __require call(s) unrewritten: ${summary}`,
    );
  }
  if (replaced > 0) {
    console.log(
      `[verself:rewrite-cjs-require] rewrote ${replaced} __require() call(s) across ${touchedFiles} chunk(s) under ${path.relative(nitro.options.rootDir, serverDir) || serverDir}`,
    );
  }
}

interface ChunkRewrite {
  readonly source: string;
  readonly replacements: number;
  readonly missed: ReadonlySet<string>;
}

function rewriteChunk(source: string): ChunkRewrite {
  const aliases = collectAliases(source);
  const missed = new Set<string>();
  let replacements = 0;
  const next = source.replace(REQUIRE_CALL_RE, (full, spec: string) => {
    if (isBuiltin(spec)) {
      return full;
    }
    const symbol = bundledSymbolFor(spec);
    if (aliases.has(symbol)) {
      replacements += 1;
      return `${symbol}()`;
    }
    missed.add(spec);
    return full;
  });
  return { source: next, replacements, missed };
}

function collectAliases(source: string): ReadonlySet<string> {
  const aliases = new Set<string>();
  for (const stmt of source.matchAll(REQUIRE_IMPORT_RE)) {
    const inside = stmt[1];
    if (inside === undefined) continue;
    for (const alias of inside.matchAll(ALIAS_RE)) {
      const symbol = alias[1];
      if (symbol !== undefined) {
        aliases.add(symbol);
      }
    }
  }
  return aliases;
}

function bundledSymbolFor(spec: string): string {
  const last = spec.split("/").pop() ?? spec;
  const safe = last.replace(/[^A-Za-z0-9_]/g, "_");
  return `require_${safe}`;
}

async function walk(dir: string, into: string[]): Promise<void> {
  let entries;
  try {
    entries = await fs.readdir(dir, { withFileTypes: true });
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") {
      return;
    }
    throw error;
  }
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      await walk(full, into);
    } else if (SCAN_EXTS.some((ext) => entry.name.endsWith(ext))) {
      into.push(full);
    }
  }
}
