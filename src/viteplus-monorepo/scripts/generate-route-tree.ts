#!/usr/bin/env node
// Generates TanStack Router's route tree as a declared Bazel output.

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
const config = getConfig(
  {
    disableLogging: true,
    generatedRouteTree: arg("generated-route-tree"),
    routesDirectory: arg("routes-directory"),
  },
  root,
);

await new Generator({ config, root }).run();
