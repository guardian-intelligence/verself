#!/usr/bin/env node
// Generates a typed TypeScript transport module from GLSL source files.

import { createHash } from "node:crypto";
import { access, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";

interface GlslModule {
  readonly vertex: string;
  readonly fragment: string;
  readonly out: string;
  readonly sources: ReadonlyMap<string, string>;
}

interface UniformDefinition {
  readonly name: string;
  readonly type: string;
}

const glsl3RejectedTokens = [/\battribute\b/, /\bvarying\b/, /\bgl_FragColor\b/, /\btexture2D\b/];

function arg(name: string): string {
  const prefix = `--${name}=`;
  const hit = process.argv.find((candidate) => candidate.startsWith(prefix));
  if (!hit) {
    throw new Error(`generate-glsl-module: missing required arg --${name}=`);
  }
  return hit.slice(prefix.length);
}

function configFromArgs(): GlslModule {
  const sources = new Map<string, string>();
  for (const candidate of process.argv) {
    const prefix = "--source=";
    if (!candidate.startsWith(prefix)) {
      continue;
    }
    const pair = candidate.slice(prefix.length);
    const separator = pair.indexOf("=");
    if (separator <= 0) {
      throw new Error(`generate-glsl-module: malformed --source arg ${candidate}`);
    }
    sources.set(normalizeModulePath(pair.slice(0, separator)), pair.slice(separator + 1));
  }
  if (sources.size === 0) {
    throw new Error("generate-glsl-module: at least one --source=<module>=<path> arg is required");
  }
  return {
    vertex: normalizeModulePath(arg("vertex")),
    fragment: normalizeModulePath(arg("fragment")),
    out: path.resolve(arg("out")),
    sources,
  };
}

function normalizeModulePath(input: string): string {
  const normalized = path.posix.normalize(input.replaceAll("\\", "/"));
  if (normalized === "." || normalized.startsWith("../") || path.posix.isAbsolute(normalized)) {
    throw new Error(`invalid GLSL module path: ${input}`);
  }
  return normalized;
}

async function resolveSource(
  sources: ReadonlyMap<string, string>,
  relativePath: string,
  stack: ReadonlyArray<string>,
): Promise<string> {
  const modulePath = normalizeModulePath(relativePath);
  const sourcePath = sources.get(modulePath);
  if (sourcePath === undefined) {
    throw new Error(`GLSL source not declared to Bazel: ${modulePath}`);
  }
  if (stack.includes(modulePath)) {
    const cycle = [...stack, modulePath].join(" -> ");
    throw new Error(`GLSL include cycle: ${cycle}`);
  }

  const source = await readFile(await resolveInputPath(sourcePath), "utf8");
  const includePattern = /^(\s*)#include\s+"([^"]+)"\s*$/gm;
  let output = "";
  let offset = 0;

  for (const match of source.matchAll(includePattern)) {
    const full = match[0];
    const start = match.index;
    const includePath = match[2];
    if (start === undefined || includePath === undefined) {
      throw new Error(`malformed include in ${relativePath}`);
    }
    output += source.slice(offset, start);
    const nested = path.posix.join(path.posix.dirname(modulePath), includePath);
    output += await resolveSource(sources, nested, [...stack, modulePath]);
    output += "\n";
    offset = start + full.length;
  }

  output += source.slice(offset);
  return output;
}

async function resolveInputPath(inputPath: string): Promise<string> {
  if (path.isAbsolute(inputPath)) {
    return inputPath;
  }

  let current = process.cwd();
  while (true) {
    const candidate = path.join(current, inputPath);
    try {
      await access(candidate);
      return candidate;
    } catch {
      const parent = path.dirname(current);
      if (parent === current) {
        throw new Error(`Bazel input path not found from ${process.cwd()}: ${inputPath}`);
      }
      current = parent;
    }
  }
}

function validateGlsl3Source(kind: "vertex" | "fragment", source: string): void {
  if (source.includes("#version")) {
    throw new Error(`${kind} shader must omit #version; Three injects the GLSL3 version line`);
  }
  for (const token of glsl3RejectedTokens) {
    if (token.test(source)) {
      throw new Error(`${kind} shader contains GLSL100 token ${token}`);
    }
  }
  if (kind === "fragment" && !/\bout\s+vec4\s+\w+\s*;/.test(source)) {
    throw new Error("fragment shader must declare an explicit vec4 output");
  }
}

function extractUniforms(source: string): ReadonlyArray<UniformDefinition> {
  const uniforms = new Map<string, string>();
  const uniformPattern = /^\s*uniform\s+([A-Za-z][A-Za-z0-9_]*)\s+([A-Za-z][A-Za-z0-9_]*)\s*;/gm;

  for (const match of source.matchAll(uniformPattern)) {
    const type = match[1];
    const name = match[2];
    if (type === undefined || name === undefined) {
      throw new Error("malformed uniform declaration");
    }
    const existing = uniforms.get(name);
    if (existing !== undefined && existing !== type) {
      throw new Error(`uniform ${name} declared as both ${existing} and ${type}`);
    }
    uniforms.set(name, type);
  }

  return [...uniforms.entries()].map(([name, type]) => ({ name, type }));
}

function emitModule(
  vertex: string,
  fragment: string,
  uniforms: ReadonlyArray<UniformDefinition>,
): string {
  const hash = createHash("sha256").update(vertex).update("\0").update(fragment).digest("hex");
  const uniformLines = uniforms
    .map((uniform) => `  ${objectKey(uniform.name)}: ${JSON.stringify(uniform.type)},`)
    .join("\n");

  return [
    "// @ts-nocheck",
    "// Generated by //src/viteplus-monorepo/scripts:generate_glsl_module.",
    "// Edit shader-src/*.glsl and run the app's dev_update target instead.",
    "",
    "export const firstLightShaderSourceHash =",
    `  ${JSON.stringify(hash)};`,
    "export const firstLightVertexShader =",
    `  ${JSON.stringify(vertex)};`,
    "export const firstLightFragmentShader =",
    `  ${JSON.stringify(fragment)};`,
    "export const firstLightUniformTypes = {",
    uniformLines,
    "} satisfies Record<string, string>;",
    "export type FirstLightUniformName = keyof typeof firstLightUniformTypes;",
    "",
  ].join("\n");
}

function objectKey(key: string): string {
  return /^[A-Za-z_$][A-Za-z0-9_$]*$/.test(key) ? key : JSON.stringify(key);
}

const config = configFromArgs();
const [vertex, fragment] = await Promise.all([
  resolveSource(config.sources, config.vertex, []),
  resolveSource(config.sources, config.fragment, []),
]);
validateGlsl3Source("vertex", vertex);
validateGlsl3Source("fragment", fragment);
const uniforms = extractUniforms(`${vertex}\n${fragment}`);
const output = emitModule(vertex, fragment, uniforms);
await mkdir(path.dirname(config.out), { recursive: true });
await writeFile(config.out, output);
