#!/usr/bin/env node
// js-x-ray AST scanner worker.
// Scans a directory of extracted npm package files for obfuscation,
// eval chains, data exfiltration, and other code-level attack patterns.
//
// Usage: node jsxray-worker.mjs <directory>
// Output: JSON to stdout with { warnings: [...] }

import { AstAnalyser } from "@nodesecure/js-x-ray";
import { readdir, readFile } from "node:fs/promises";
import { join, extname } from "node:path";

const JS_EXTENSIONS = new Set([".js", ".mjs", ".cjs"]);
const MAX_FILE_SIZE = 2 * 1024 * 1024; // 2MB — skip minified bundles larger than this.

const dir = process.argv[2];
if (!dir) {
  console.error("usage: jsxray-worker.mjs <directory>");
  process.exit(1);
}

const analyser = new AstAnalyser();
const warnings = [];

async function walk(dirPath) {
  let entries;
  try {
    entries = await readdir(dirPath, { withFileTypes: true });
  } catch {
    return;
  }
  for (const entry of entries) {
    const fullPath = join(dirPath, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "node_modules" || entry.name === ".git") continue;
      await walk(fullPath);
    } else if (JS_EXTENSIONS.has(extname(entry.name))) {
      await scanFile(fullPath);
    }
  }
}

async function scanFile(filePath) {
  try {
    const source = await readFile(filePath, "utf-8");
    if (source.length > MAX_FILE_SIZE) return;

    const report = analyser.analyse(source);
    for (const w of report.warnings) {
      warnings.push({
        kind: w.kind,
        severity: w.severity ?? "warning",
        file: filePath.replace(dir + "/", ""),
        detail: w.value ?? "",
      });
    }
  } catch {
    // Parse errors in non-JS files are expected; skip silently.
  }
}

await walk(dir);
process.stdout.write(JSON.stringify({ warnings }));
