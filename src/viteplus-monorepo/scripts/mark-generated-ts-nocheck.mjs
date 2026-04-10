import { readdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";

const banner = "// @ts-nocheck\n";

async function visit(entryPath) {
  const entryStat = await stat(entryPath);

  if (entryStat.isDirectory()) {
    const entries = await readdir(entryPath);
    await Promise.all(entries.map((entry) => visit(path.join(entryPath, entry))));
    return;
  }

  if (!entryPath.endsWith(".ts")) {
    return;
  }

  const source = await readFile(entryPath, "utf8");
  if (source.startsWith(banner)) {
    return;
  }

  await writeFile(entryPath, `${banner}${source}`);
}

const targets = process.argv.slice(2);
if (targets.length === 0) {
  throw new Error("expected at least one generated directory");
}

await Promise.all(targets.map((target) => visit(path.resolve(target))));
