// Copies an openapi-ts output directory to a new location and prepends a
// `// @ts-nocheck` banner to every .ts file. The banner is required because
// upstream openapi-ts emits TypeScript that doesn't pass our project's
// strict tsconfig; vite/rollup don't typecheck at build time, but
// `vp run typecheck` does and would otherwise fail on generated code.
//
// Usage: node openapi-postprocess.mjs <input_dir> <output_dir>

import { mkdir, readdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";

const banner = "// @ts-nocheck\n";

async function copyAndStamp(srcDir, dstDir) {
  await mkdir(dstDir, { recursive: true });
  const entries = await readdir(srcDir);
  for (const entry of entries) {
    const srcPath = path.join(srcDir, entry);
    const dstPath = path.join(dstDir, entry);
    const entryStat = await stat(srcPath);
    if (entryStat.isDirectory()) {
      await copyAndStamp(srcPath, dstPath);
      continue;
    }
    if (entry.endsWith(".ts")) {
      const source = await readFile(srcPath, "utf8");
      const stamped = source.startsWith(banner) ? source : `${banner}${source}`;
      await writeFile(dstPath, stamped);
      continue;
    }
    const buf = await readFile(srcPath);
    await writeFile(dstPath, buf);
  }
}

const [, , inputDir, outputDir] = process.argv;
if (!inputDir || !outputDir) {
  throw new Error("expected: node openapi-postprocess.mjs <input_dir> <output_dir>");
}

await copyAndStamp(path.resolve(inputDir), path.resolve(outputDir));
