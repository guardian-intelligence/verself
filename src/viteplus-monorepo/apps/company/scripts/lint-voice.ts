import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { assertVoice, formatViolation } from "../src/brand/voice.ts";

const ROOTS = ["src/content"];
const EXTENSIONS = new Set([".md", ".mdx", ".ts", ".tsx"]);

async function* walk(dir: string): AsyncGenerator<string> {
  const entries = await readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      yield* walk(fullPath);
      continue;
    }
    if (entry.isFile() && EXTENSIONS.has(path.extname(entry.name))) {
      yield fullPath;
    }
  }
}

let failures = 0;
for (const root of ROOTS) {
  for await (const filePath of walk(root)) {
    const text = await readFile(filePath, "utf8");
    const result = assertVoice(text, filePath);
    if (result.ok) {
      continue;
    }
    failures += result.violations.length;
    for (const violation of result.violations) {
      console.error(formatViolation(violation));
    }
  }
}

if (failures > 0) {
  console.error(`voice lint failed with ${failures} violation(s)`);
  process.exit(1);
}
