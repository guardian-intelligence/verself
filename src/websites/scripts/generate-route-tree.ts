#!/usr/bin/env node
// Generates TanStack Router's route tree as a declared Bazel output.

import { readFile, writeFile } from "node:fs/promises";
import process from "node:process";

import { Generator, getConfig } from "@tanstack/router-generator";

function arg(name: string): string {
  const prefix = `--${name}=`;
  const hit = process.argv.find((candidate) => candidate.startsWith(prefix));
  if (!hit) {
    throw new Error(`generate-route-tree: missing required arg --${name}=`);
  }
  return hit.slice(prefix.length);
}

const root = process.cwd();
const generatedRouteTree = arg("generated-route-tree");
const config = getConfig(
  {
    disableLogging: true,
    generatedRouteTree,
    routesDirectory: arg("routes-directory"),
  },
  root,
);

await new Generator({ config, root }).run();

if (generatedRouteTree.includes("__generated_sources/src/routeTree.gen.ts")) {
  const generatedPath = new URL(generatedRouteTree, `file://${root}/`);
  let contents = await readFile(generatedPath, "utf8");

  // TanStack derives import paths from the generated file location; Bazel keeps
  // declared outputs outside the source tree and projects them back afterward.
  contents = contents
    .replaceAll("from './../../src/routes/", "from './routes/")
    .replaceAll('from "./../../src/routes/', 'from "./routes/')
    .replaceAll("from './../../src/router.tsx'", "from './router.tsx'")
    .replaceAll('from "./../../src/router.tsx"', 'from "./router.tsx"');

  if (!contents.includes("declare module '@tanstack/react-start'")) {
    contents = `${contents.trimEnd()}

import type { getRouter } from './router.tsx'
import type { createStart } from '@tanstack/react-start'
declare module '@tanstack/react-start' {
  interface Register {
    ssr: true
    router: Awaited<ReturnType<typeof getRouter>>
  }
}
`;
  }

  await writeFile(generatedPath, contents);
}
