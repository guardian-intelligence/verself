// Voice lint. Scans every string export under src/content/** and fails non-zero
// on any banned word or BuzzFeed hook. Wired into `pnpm -F @forge-metal/company
// lint:voice` and CI.
//
// Strategy: dynamic-import each `content/**/*.{ts,mts}` file, walk every
// exported value recursively, and run assertVoice() on every string it finds.
// Arrays, objects, and Maps descend; everything else is scanned as-is.

import { readdir } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { assertVoice, formatViolation } from "../src/brand/voice.ts";

const CONTENT_ROOT = path.resolve(import.meta.dirname, "..", "src", "content");

async function listContentFiles(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const out: string[] = [];
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...(await listContentFiles(full)));
      continue;
    }
    if (entry.isFile() && /\.(ts|mts|tsx)$/.test(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

type Walked = { readonly path: string; readonly value: string };

function walk(prefix: string, value: unknown, out: Walked[]): void {
  if (value == null) return;
  if (typeof value === "string") {
    out.push({ path: prefix, value });
    return;
  }
  if (Array.isArray(value)) {
    value.forEach((item, idx) => walk(`${prefix}[${idx}]`, item, out));
    return;
  }
  if (typeof value === "object") {
    for (const [key, nested] of Object.entries(value as Record<string, unknown>)) {
      walk(prefix ? `${prefix}.${key}` : key, nested, out);
    }
  }
}

async function main(): Promise<void> {
  const files = await listContentFiles(CONTENT_ROOT);
  if (files.length === 0) {
    console.error(`lint-voice: no files under ${CONTENT_ROOT}`);
    process.exit(1);
  }

  let totalStrings = 0;
  let totalViolations = 0;

  for (const file of files) {
    const relative = path.relative(CONTENT_ROOT, file);
    const mod = (await import(pathToFileURL(file).href)) as Record<string, unknown>;
    const strings: Walked[] = [];
    for (const [exportName, exportValue] of Object.entries(mod)) {
      walk(exportName, exportValue, strings);
    }
    for (const item of strings) {
      totalStrings += 1;
      const result = assertVoice(item.value, `${relative}#${item.path}`);
      if (!result.ok) {
        totalViolations += result.violations.length;
        for (const violation of result.violations) {
          console.error(`voice_violation: ${formatViolation(violation)}`);
          console.error(`  at ${relative}#${item.path}`);
          console.error(`  text: ${item.value}`);
        }
      }
    }
  }

  if (totalViolations > 0) {
    console.error(
      `lint-voice: ${totalViolations} violation(s) across ${totalStrings} string(s) in ${files.length} file(s).`,
    );
    process.exit(1);
  }
  console.log(
    `lint-voice: OK — ${totalStrings} string(s) across ${files.length} file(s) in src/content/.`,
  );
}

void main().catch((error: unknown) => {
  console.error("lint-voice: unexpected error", error);
  process.exit(2);
});
