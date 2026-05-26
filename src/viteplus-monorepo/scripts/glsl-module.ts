import { createHash } from "node:crypto";
import { access, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import process from "node:process";

export interface GlslModuleSource {
  readonly filePath: string;
  readonly modulePath: string;
}

export interface GlslModuleConfig {
  readonly cwd?: string;
  readonly fragment: string;
  readonly out: string;
  readonly sources: readonly GlslModuleSource[] | ReadonlyMap<string, string>;
  readonly symbolPrefix: string;
  readonly vertex: string;
}

export interface GlslModuleResult {
  readonly changed: boolean;
  readonly out: string;
  readonly sourceHash: string;
}

interface ResolvedGlslModule {
  readonly cwd: string;
  readonly fragment: string;
  readonly out: string;
  readonly sources: ReadonlyMap<string, string>;
  readonly symbolPrefix: string;
  readonly vertex: string;
}

interface UniformDefinition {
  readonly name: string;
  readonly type: string;
}

const glsl3RejectedTokens = [/\battribute\b/, /\bvarying\b/, /\bgl_FragColor\b/, /\btexture2D\b/];

export function glslModuleConfigFromArgs(args: readonly string[]): GlslModuleConfig {
  const sources: GlslModuleSource[] = [];
  for (const candidate of args) {
    const prefix = "--source=";
    if (!candidate.startsWith(prefix)) {
      continue;
    }
    const pair = candidate.slice(prefix.length);
    const separator = pair.indexOf("=");
    if (separator <= 0) {
      throw new Error(`generate-glsl-module: malformed --source arg ${candidate}`);
    }
    sources.push({
      filePath: pair.slice(separator + 1),
      modulePath: normalizeModulePath(pair.slice(0, separator)),
    });
  }
  if (sources.length === 0) {
    throw new Error("generate-glsl-module: at least one --source=<module>=<path> arg is required");
  }
  return {
    fragment: normalizeModulePath(requiredArg(args, "fragment")),
    out: path.resolve(requiredArg(args, "out")),
    sources,
    symbolPrefix: validateSymbolPrefix(optionalArg(args, "symbol-prefix", "firstLight")),
    vertex: normalizeModulePath(requiredArg(args, "vertex")),
  };
}

export async function generateGlslModule(config: GlslModuleConfig): Promise<GlslModuleResult> {
  const resolvedConfig = resolveConfig(config);
  const [vertex, fragment] = await Promise.all([
    resolveSource(resolvedConfig, resolvedConfig.vertex, []),
    resolveSource(resolvedConfig, resolvedConfig.fragment, []),
  ]);
  validateGlsl3Source("vertex", vertex);
  validateGlsl3Source("fragment", fragment);

  const uniforms = extractUniforms(`${vertex}\n${fragment}`);
  const output = emitModule(vertex, fragment, uniforms, resolvedConfig.symbolPrefix);
  await mkdir(path.dirname(resolvedConfig.out), { recursive: true });
  const changed = await writeIfChanged(resolvedConfig.out, output);
  const sourceHash = createSourceHash(vertex, fragment);
  return { changed, out: resolvedConfig.out, sourceHash };
}

function requiredArg(args: readonly string[], name: string): string {
  const prefix = `--${name}=`;
  const hit = args.find((candidate) => candidate.startsWith(prefix));
  if (!hit) {
    throw new Error(`generate-glsl-module: missing required arg --${name}=`);
  }
  return hit.slice(prefix.length);
}

function optionalArg(args: readonly string[], name: string, fallback: string): string {
  const prefix = `--${name}=`;
  const hit = args.find((candidate) => candidate.startsWith(prefix));
  return hit ? hit.slice(prefix.length) : fallback;
}

function resolveConfig(config: GlslModuleConfig): ResolvedGlslModule {
  return {
    cwd: config.cwd ?? process.cwd(),
    fragment: normalizeModulePath(config.fragment),
    out: path.resolve(config.cwd ?? process.cwd(), config.out),
    sources: normalizeSources(config.sources),
    symbolPrefix: validateSymbolPrefix(config.symbolPrefix),
    vertex: normalizeModulePath(config.vertex),
  };
}

function normalizeSources(
  sources: readonly GlslModuleSource[] | ReadonlyMap<string, string>,
): ReadonlyMap<string, string> {
  if (isSourceMap(sources)) {
    return new Map(
      [...sources.entries()].map(([modulePath, filePath]) => [
        normalizeModulePath(modulePath),
        filePath,
      ]),
    );
  }

  return new Map(
    sources.map((source) => [normalizeModulePath(source.modulePath), source.filePath] as const),
  );
}

function isSourceMap(
  sources: readonly GlslModuleSource[] | ReadonlyMap<string, string>,
): sources is ReadonlyMap<string, string> {
  return typeof (sources as ReadonlyMap<string, string>).get === "function";
}

function normalizeModulePath(input: string): string {
  const normalized = path.posix.normalize(input.replaceAll("\\", "/"));
  if (normalized === "." || normalized.startsWith("../") || path.posix.isAbsolute(normalized)) {
    throw new Error(`invalid GLSL module path: ${input}`);
  }
  return normalized;
}

function validateSymbolPrefix(input: string): string {
  if (!/^[A-Za-z_$][A-Za-z0-9_$]*$/.test(input)) {
    throw new Error(`invalid exported symbol prefix: ${input}`);
  }
  return input;
}

async function resolveSource(
  config: ResolvedGlslModule,
  relativePath: string,
  stack: readonly string[],
): Promise<string> {
  const modulePath = normalizeModulePath(relativePath);
  const sourcePath = config.sources.get(modulePath);
  if (sourcePath === undefined) {
    throw new Error(`GLSL source not declared to generator: ${modulePath}`);
  }
  if (stack.includes(modulePath)) {
    const cycle = [...stack, modulePath].join(" -> ");
    throw new Error(`GLSL include cycle: ${cycle}`);
  }

  const source = await readFile(await resolveInputPath(sourcePath, config.cwd), "utf8");
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
    output += await resolveSource(config, nested, [...stack, modulePath]);
    output += "\n";
    offset = start + full.length;
  }

  output += source.slice(offset);
  return output;
}

async function resolveInputPath(inputPath: string, cwd: string): Promise<string> {
  if (path.isAbsolute(inputPath)) {
    return inputPath;
  }

  let current = cwd;
  while (true) {
    const candidate = path.join(current, inputPath);
    try {
      await access(candidate);
      return candidate;
    } catch {
      const parent = path.dirname(current);
      if (parent === current) {
        throw new Error(`GLSL input path not found from ${cwd}: ${inputPath}`);
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

function extractUniforms(source: string): readonly UniformDefinition[] {
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
  uniforms: readonly UniformDefinition[],
  symbolPrefix: string,
): string {
  const hash = createSourceHash(vertex, fragment);
  const uniformLines = uniforms
    .map((uniform) => `  ${objectKey(uniform.name)}: ${JSON.stringify(uniform.type)},`)
    .join("\n");
  const typePrefix = `${symbolPrefix[0]?.toUpperCase()}${symbolPrefix.slice(1)}`;

  return [
    "// @ts-nocheck",
    "// Generated by //src/viteplus-monorepo/scripts:generate_glsl_module and the app Vite pipeline.",
    "// Edit shader-src/*; dev and build commands regenerate this file.",
    "",
    `export const ${symbolPrefix}ShaderSourceHash =`,
    `  ${JSON.stringify(hash)};`,
    `export const ${symbolPrefix}VertexShader =`,
    `  ${JSON.stringify(vertex)};`,
    `export const ${symbolPrefix}FragmentShader =`,
    `  ${JSON.stringify(fragment)};`,
    `export const ${symbolPrefix}UniformTypes = {`,
    uniformLines,
    "} satisfies Record<string, string>;",
    `export type ${typePrefix}UniformName = keyof typeof ${symbolPrefix}UniformTypes;`,
    "",
  ].join("\n");
}

function createSourceHash(vertex: string, fragment: string): string {
  return createHash("sha256").update(vertex).update("\0").update(fragment).digest("hex");
}

function objectKey(key: string): string {
  return /^[A-Za-z_$][A-Za-z0-9_$]*$/.test(key) ? key : JSON.stringify(key);
}

async function writeIfChanged(out: string, output: string): Promise<boolean> {
  try {
    if ((await readFile(out, "utf8")) === output) {
      return false;
    }
  } catch {
    // Missing output is expected on clean checkouts and first dev runs.
  }
  await writeFile(out, output);
  return true;
}
