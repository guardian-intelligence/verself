import path from "node:path";
import type { ModuleNode, Plugin, ResolvedConfig, ViteDevServer } from "vite";

import { generateGlslModule, type GlslModuleSource } from "../../scripts/glsl-module.ts";

interface DotMatrixShaderModule {
  readonly fragment: string;
  readonly out: string;
  readonly symbolPrefix: string;
  readonly vertex: string;
}

const shaderSrcDir = "src/features/landing/dot-matrix/shader-src";
const shaderOutDir = "src/features/landing/dot-matrix/shader";

const shaderSources = [
  { modulePath: "dot-matrix.vert", filePath: `${shaderSrcDir}/dot-matrix.vert` },
  { modulePath: "dot-matrix.frag", filePath: `${shaderSrcDir}/dot-matrix.frag` },
  { modulePath: "wave-simulation.frag", filePath: `${shaderSrcDir}/wave-simulation.frag` },
] satisfies readonly GlslModuleSource[];

const shaderModules = [
  {
    fragment: "dot-matrix.frag",
    out: `${shaderOutDir}/dot-matrix.generated.ts`,
    symbolPrefix: "dotMatrix",
    vertex: "dot-matrix.vert",
  },
  {
    fragment: "wave-simulation.frag",
    out: `${shaderOutDir}/wave-simulation.generated.ts`,
    symbolPrefix: "dotMatrixWave",
    vertex: "dot-matrix.vert",
  },
] satisfies readonly DotMatrixShaderModule[];

export function dotMatrixShaderGeneration(): Plugin {
  let config: ResolvedConfig | undefined;
  let server: ViteDevServer | undefined;

  const root = () => {
    if (config === undefined) {
      throw new Error("dot matrix shader generation used before Vite config resolution");
    }
    return config.root;
  };

  const toAbsolutePath = (filePath: string) => path.resolve(root(), filePath);
  const normalized = (filePath: string) => path.normalize(filePath);
  const allSourcePaths = () => shaderSources.map((source) => toAbsolutePath(source.filePath));
  const allSourcePathSet = () => new Set(allSourcePaths().map(normalized));

  const sourcePathForModule = (modulePath: string) => {
    const source = shaderSources.find((candidate) => candidate.modulePath === modulePath);
    if (source === undefined) {
      throw new Error(`unknown dot matrix shader source: ${modulePath}`);
    }
    return normalized(toAbsolutePath(source.filePath));
  };

  const moduleSourcePaths = (module: DotMatrixShaderModule) =>
    new Set([module.vertex, module.fragment].map((modulePath) => sourcePathForModule(modulePath)));

  const affectedModules = (filePath: string) => {
    const changed = normalized(filePath);
    if (!allSourcePathSet().has(changed)) return [];

    const directlyAffected = shaderModules.filter((module) =>
      moduleSourcePaths(module).has(changed),
    );
    return directlyAffected.length > 0 ? directlyAffected : shaderModules;
  };

  const generatedModulesFor = (modules: readonly DotMatrixShaderModule[]): ModuleNode[] => {
    if (server === undefined) return [];
    const nodes: ModuleNode[] = [];
    for (const module of modules) {
      const out = toAbsolutePath(module.out);
      const generated = server.moduleGraph.getModulesByFile(out);
      if (generated !== undefined) nodes.push(...generated);
    }
    return nodes;
  };

  const generate = async (modules: readonly DotMatrixShaderModule[]) => {
    for (const module of modules) {
      await generateGlslModule({
        cwd: root(),
        fragment: module.fragment,
        out: module.out,
        sources: shaderSources,
        symbolPrefix: module.symbolPrefix,
        vertex: module.vertex,
      });
    }
  };

  return {
    name: "verself-web:dot-matrix-shader-generation",
    enforce: "pre",

    configResolved(resolvedConfig) {
      config = resolvedConfig;
    },

    async buildStart() {
      for (const sourcePath of allSourcePaths()) {
        this.addWatchFile(sourcePath);
      }
      await generate(shaderModules);
    },

    configureServer(viteServer) {
      server = viteServer;
      viteServer.watcher.add(allSourcePaths());
    },

    async handleHotUpdate(ctx) {
      const modules = affectedModules(ctx.file);
      if (modules.length === 0) return;

      await generate(modules);
      const generated = generatedModulesFor(modules);
      if (generated.length > 0) return generated;
      ctx.server.ws.send({ type: "full-reload" });
      return [];
    },
  };
}
