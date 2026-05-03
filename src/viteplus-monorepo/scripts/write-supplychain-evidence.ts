#!/usr/bin/env node
// Writes an in-toto/SLSA-shaped evidence envelope for explicitly named files.
// It hashes bytes and records metadata; scanners and admission policy live elsewhere.

import { createHash } from "node:crypto";
import { lstat, mkdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";

interface TemporalInstant {
  toString(): string;
}

interface TemporalNamespace {
  Instant: {
    from(value: string): TemporalInstant;
  };
}

interface NamedPath {
  name: string;
  path: string;
}

interface Args {
  byproducts: NamedPath[];
  dependencies: NamedPath[];
  externalParameters: Record<string, string>;
  help?: boolean;
  internalParameters: Record<string, string>;
  subjects: NamedPath[];
  versions: Record<string, string>;
  buildType?: string;
  builderID?: string;
  finishedOn?: string;
  invocationID?: string;
  out?: string;
  startedOn?: string;
}

interface FileDescriptor {
  annotations: Record<string, string>;
  digest: {
    sha256: string;
  };
  name: string;
}

const statementType = "https://in-toto.io/Statement/v1";
const predicateType = "https://slsa.dev/provenance/v1";

function emptyArgs(): Args {
  return {
    byproducts: [],
    dependencies: [],
    externalParameters: {},
    internalParameters: {},
    subjects: [],
    versions: {},
  };
}

function parseArgs(argv: string[]): Args {
  const args = emptyArgs();
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
    if (key === "out") {
      args.out = value;
      continue;
    }
    if (key === "build-type") {
      args.buildType = value;
      continue;
    }
    if (key === "builder-id") {
      args.builderID = value;
      continue;
    }
    if (key === "invocation-id") {
      args.invocationID = value;
      continue;
    }
    if (key === "started-on") {
      args.startedOn = parseTimestamp(value, "--started-on");
      continue;
    }
    if (key === "finished-on") {
      args.finishedOn = parseTimestamp(value, "--finished-on");
      continue;
    }
    if (key === "subject") {
      args.subjects.push(parseNamedPath(value, "--subject"));
      continue;
    }
    if (key === "byproduct") {
      args.byproducts.push(parseNamedPath(value, "--byproduct"));
      continue;
    }
    if (key === "dependency") {
      args.dependencies.push(parseNamedPath(value, "--dependency"));
      continue;
    }
    if (key === "external-param") {
      addKeyValue(args.externalParameters, value, "--external-param");
      continue;
    }
    if (key === "internal-param") {
      addKeyValue(args.internalParameters, value, "--internal-param");
      continue;
    }
    if (key === "builder-version") {
      addKeyValue(args.versions, value, "--builder-version");
      continue;
    }
    throw new Error(`unknown argument --${key}`);
  }
  return args;
}

function usage(): string {
  return [
    "Usage:",
    "  node write-supplychain-evidence.ts \\",
    "    --out=<statement.json> \\",
    "    --build-type=<type-uri> \\",
    "    --builder-id=<builder-uri> \\",
    "    --subject=<name>=<path> \\",
    "    [--byproduct=<name>=<path>] \\",
    "    [--dependency=<name>=<path>]",
    "",
    "Optional metadata:",
    "  --external-param=<key>=<value>",
    "  --internal-param=<key>=<value>",
    "  --builder-version=<key>=<value>",
    "  --invocation-id=<id>",
    "  --started-on=<RFC3339 UTC timestamp>",
    "  --finished-on=<RFC3339 UTC timestamp>",
    "",
    "Temporal is required only when timestamp arguments are provided.",
  ].join("\n");
}

function requireArg(args: Args, name: "buildType" | "builderID" | "out"): string {
  const value = args[name];
  if (!value) throw new Error(`missing required arg --${name}=`);
  return value;
}

function parseNamedPath(value: string, flag: string): NamedPath {
  const separator = value.indexOf("=");
  if (separator <= 0 || separator === value.length - 1) {
    throw new Error(`${flag} must be <name>=<path>`);
  }
  const name = value.slice(0, separator);
  const filePath = value.slice(separator + 1);
  if (!/^[A-Za-z0-9._/@:+-]+$/.test(name)) {
    throw new Error(`${flag} name contains unsupported characters: ${name}`);
  }
  return { name, path: filePath };
}

function addKeyValue(target: Record<string, string>, value: string, flag: string): void {
  const separator = value.indexOf("=");
  if (separator <= 0) throw new Error(`${flag} must be <key>=<value>`);
  const key = value.slice(0, separator);
  const nestedValue = value.slice(separator + 1);
  if (!/^[A-Za-z0-9._/-]+$/.test(key)) {
    throw new Error(`${flag} key contains unsupported characters: ${key}`);
  }
  if (Object.hasOwn(target, key)) {
    throw new Error(`${flag} duplicates key ${key}`);
  }
  target[key] = nestedValue;
}

function temporal(flag: string): TemporalNamespace {
  const candidate = (globalThis as typeof globalThis & { Temporal?: TemporalNamespace }).Temporal;
  if (!candidate) {
    throw new Error(
      `${flag} requires Temporal; run Node with --harmony-temporal or pin a Vite+-managed Node runtime with Temporal enabled`,
    );
  }
  return candidate;
}

function parseTimestamp(value: string, flag: string): string {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(value)) {
    throw new Error(`${flag} must be an RFC3339 UTC timestamp with seconds`);
  }
  const temporalInstant = temporal(flag).Instant;
  try {
    const instant = temporalInstant.from(value);
    if (instant.toString() !== value) throw new Error("timestamp must be canonical UTC");
  } catch {
    throw new Error(`${flag} is not a valid Temporal.Instant timestamp`);
  }
  return value;
}

function validateArgs(args: Args): void {
  requireArg(args, "out");
  requireArg(args, "buildType");
  requireArg(args, "builderID");
  if (args.subjects.length === 0) throw new Error("at least one --subject is required");
  ensureUniqueNames("subject", args.subjects);
  ensureUniqueNames("byproduct", args.byproducts);
  ensureUniqueNames("dependency", args.dependencies);
}

function ensureUniqueNames(label: string, descriptors: NamedPath[]): void {
  const names = new Set<string>();
  for (const descriptor of descriptors) {
    if (names.has(descriptor.name)) throw new Error(`duplicate ${label} name ${descriptor.name}`);
    names.add(descriptor.name);
  }
}

async function descriptorFor(namedPath: NamedPath, outPath: string): Promise<FileDescriptor> {
  const absolutePath = path.resolve(namedPath.path);
  if (absolutePath === outPath) throw new Error(`input ${namedPath.name} points at --out`);
  const linkStat = await lstat(absolutePath);
  if (linkStat.isSymbolicLink()) {
    throw new Error(`refusing to hash symlinked evidence input ${namedPath.path}`);
  }
  const fileStat = await stat(absolutePath);
  if (!fileStat.isFile())
    throw new Error(`evidence input is not a regular file: ${namedPath.path}`);
  const bytes = await readFile(absolutePath);
  return {
    annotations: {
      "verself.size": String(fileStat.size),
    },
    digest: {
      sha256: createHash("sha256").update(bytes).digest("hex"),
    },
    name: namedPath.name,
  };
}

function metadata(args: Args): Record<string, string> {
  const result: Record<string, string> = {};
  if (args.finishedOn) result.finishedOn = args.finishedOn;
  if (args.invocationID) result.invocationId = args.invocationID;
  if (args.startedOn) result.startedOn = args.startedOn;
  return result;
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
  validateArgs(args);

  const outPath = path.resolve(requireArg(args, "out"));
  const subjects: FileDescriptor[] = [];
  for (const subject of args.subjects) subjects.push(await descriptorFor(subject, outPath));

  const byproducts: FileDescriptor[] = [];
  for (const byproduct of args.byproducts) byproducts.push(await descriptorFor(byproduct, outPath));

  const dependencies: FileDescriptor[] = [];
  for (const dependency of args.dependencies)
    dependencies.push(await descriptorFor(dependency, outPath));

  const statement = {
    _type: statementType,
    predicate: {
      buildDefinition: {
        buildType: requireArg(args, "buildType"),
        externalParameters: args.externalParameters,
        internalParameters: args.internalParameters,
        resolvedDependencies: dependencies,
      },
      runDetails: {
        builder: {
          id: requireArg(args, "builderID"),
          version: args.versions,
        },
        byproducts,
        metadata: metadata(args),
      },
    },
    predicateType,
    subject: subjects,
  };

  await writeFileAtomic(outPath, stableJSONStringify(statement));
}

main().catch((error: unknown) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
