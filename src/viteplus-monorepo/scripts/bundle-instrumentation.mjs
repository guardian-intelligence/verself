#!/usr/bin/env node
// Bundles Node runtime entrypoints into self-contained ESM files that run
// without a node_modules sidecar inside Nomad artifact directories.
//
// Uses Rolldown — the same bundler `vp build` already runs for the server and
// client outputs — so resolution, exports-condition handling, and CJS/ESM
// interop match what the rest of the app gets.
//
// Args:
//   --entry=<path>     entrypoint (relative to the cwd Bazel sets via
//                      `chdir = native.package_name()`)
//   --outfile=<path>   bundled output file
import process from "node:process";

import { build } from "rolldown";

function arg(name) {
  const prefix = `--${name}=`;
  const hit = process.argv.find((a) => a.startsWith(prefix));
  if (!hit) {
    throw new Error(`bundle-node-entry: missing required arg --${name}=`);
  }
  return hit.slice(prefix.length);
}

await build({
  input: arg("entry"),
  platform: "node",
  external: (id) => id.startsWith("node:"),
  tsconfig: "tsconfig.json",
  output: {
    file: arg("outfile"),
    format: "esm",
    codeSplitting: false,
    banner:
      'import { createRequire as __nodeCreateRequire } from "node:module"; const require = __nodeCreateRequire(import.meta.url);',
  },
  // OTel core ships as CJS; bundling it into an ESM output drops the
  // implicit module-scope `require`. Rolldown ships a CJS-in-ESM polyfill,
  // but recreating the real Node `require` from `import.meta.url` is
  // strictly more correct: it lets bundled CJS code call
  // `require('node:util')` etc. exactly as it would in a CJS context.
});
