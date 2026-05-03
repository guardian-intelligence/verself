#!/usr/bin/env node
// Emits a deterministic integrity manifest for already-built browser assets.
// It does not parse Vite/Nitro manifests or decide CSP policy.

import { createHash } from "node:crypto";
import { lstat, mkdir, readdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";

interface Args {
  extensions: string[];
  help?: boolean;
  out?: string;
  root?: string;
}

interface Asset {
  absolutePath: string;
  manifestPath: string;
  size: number;
}

const manifestSchema = "https://verself.sh/schemas/viteplus-asset-integrity/v1";
const defaultExtensions = [".css", ".js", ".mjs", ".wasm"];

function parseArgs(argv: string[]): Args {
  const args: Args = {
    extensions: defaultExtensions,
  };
  for (const arg of argv) {
    if (arg === "--help") {
      args.help = true;
      continue;
    }
    const match = /^--([^=]+)=(.*)$/.exec(arg);
    if (!match) throw new Error(`unexpected argument ${arg}`);
    const key = match[1];
    const value = match[2];
    if (key === undefined || value === undefined) throw new Error(`malformed argument ${arg}`);
    if (key === "root") {
      args.root = value;
      continue;
    }
    if (key === "out") {
      args.out = value;
      continue;
    }
    if (key === "extensions") {
      args.extensions = parseExtensions(value);
      continue;
    }
    throw new Error(`unknown argument --${key}`);
  }
  return args;
}

function parseExtensions(value: string): string[] {
  if (!value) throw new Error("--extensions must not be empty");
  const extensions = value.split(",").map((entry) => entry.trim());
  for (const extension of extensions) {
    if (!/^\.[a-z0-9]+$/i.test(extension)) {
      throw new Error(`invalid asset extension ${extension}`);
    }
  }
  return [...new Set(extensions.map((extension) => extension.toLowerCase()))].sort();
}

function usage(): string {
  return [
    "Usage:",
    "  node emit-asset-integrity.ts --root=<asset_dir> --out=<manifest.json>",
    "",
    "Options:",
    "  --extensions=.css,.js,.mjs,.wasm",
  ].join("\n");
}

function requireArg(args: Args, name: "out" | "root"): string {
  const value = args[name];
  if (!value) throw new Error(`missing required arg --${name}=`);
  return value;
}

function isPathInside(parent: string, child: string): boolean {
  const relative = path.relative(parent, child);
  return (
    relative === "" || (!!relative && !relative.startsWith("..") && !path.isAbsolute(relative))
  );
}

function toManifestPath(filePath: string): string {
  return filePath.split(path.sep).join("/");
}

async function listAssets(root: string, extensions: string[]): Promise<Asset[]> {
  const assets: Asset[] = [];

  async function walk(relativeDir: string): Promise<void> {
    const absoluteDir = path.join(root, relativeDir);
    const entries = await readdir(absoluteDir, { withFileTypes: true });
    entries.sort((a, b) => a.name.localeCompare(b.name));

    for (const entry of entries) {
      const relativePath = path.join(relativeDir, entry.name);
      const absolutePath = path.join(root, relativePath);
      const entryStat = await lstat(absolutePath);
      if (entryStat.isSymbolicLink()) {
        throw new Error(`refusing to hash symlinked asset ${relativePath}`);
      }
      if (entryStat.isDirectory()) {
        await walk(relativePath);
        continue;
      }
      if (!entryStat.isFile()) {
        throw new Error(`refusing to hash non-file asset ${relativePath}`);
      }
      if (extensions.includes(path.extname(entry.name).toLowerCase())) {
        assets.push({
          absolutePath,
          manifestPath: toManifestPath(relativePath),
          size: entryStat.size,
        });
      }
    }
  }

  await walk("");
  return assets;
}

function digest(algorithm: string, bytes: Buffer, encoding: "base64" | "hex"): string {
  return createHash(algorithm).update(bytes).digest(encoding);
}

function stableJSONStringify(value: unknown): string {
  return `${JSON.stringify(sortJSON(value), null, 2)}\n`;
}

function sortJSON(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortJSON);
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.entries(value)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([key, nested]) => [key, sortJSON(nested)]),
  );
}

async function writeFileAtomic(outPath: string, bytes: string): Promise<void> {
  await mkdir(path.dirname(outPath), { recursive: true });
  const tmpPath = `${outPath}.tmp-${process.pid}`;
  try {
    await writeFile(tmpPath, bytes);
    await rename(tmpPath, outPath);
  } catch (error) {
    await rm(tmpPath, { force: true });
    throw error;
  }
}

async function main(): Promise<void> {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    console.log(usage());
    return;
  }

  const root = path.resolve(requireArg(args, "root"));
  const out = path.resolve(requireArg(args, "out"));
  const rootStat = await stat(root);
  if (!rootStat.isDirectory()) throw new Error(`asset root is not a directory: ${root}`);
  if (isPathInside(root, out)) {
    throw new Error("output manifest must be written outside the asset root");
  }

  const assets = await listAssets(root, args.extensions);
  if (assets.length === 0) {
    throw new Error(`no browser assets found under ${root}`);
  }

  const manifestAssets = [];
  for (const asset of assets) {
    const bytes = await readFile(asset.absolutePath);
    manifestAssets.push({
      integrity: `sha384-${digest("sha384", bytes, "base64")}`,
      path: asset.manifestPath,
      sha256: digest("sha256", bytes, "hex"),
      size: asset.size,
    });
  }

  const manifest = {
    assets: manifestAssets,
    extensions: args.extensions,
    schema: manifestSchema,
  };
  await writeFileAtomic(out, stableJSONStringify(manifest));
}

main().catch((error: unknown) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
