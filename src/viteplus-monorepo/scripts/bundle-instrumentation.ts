#!/usr/bin/env node
// Bundles Node runtime entrypoints into self-contained ESM files that run
// without a node_modules sidecar inside Nomad artifact directories.
//
// Uses Rolldown — the same bundler `vp build` already runs for the server and
// client outputs — so resolution, exports-condition handling, and CJS/ESM
// interop match what the rest of the app gets.
//
// Args:
//   --entry=<path|specifier>  entrypoint, resolved from the cwd Bazel sets via
//                             `chdir = native.package_name()` (a relative path
//                             or a node-resolvable bare specifier)
//   --outfile=<path>          bundled output file
//   --external-bare           keep every bare npm/package import external,
//                             inlining only the entry's own relative imports
import process from "node:process";

import { build } from "rolldown";

function arg(name: string): string {
  const prefix = `--${name}=`;
  const hit = process.argv.find((candidate) => candidate.startsWith(prefix));
  if (!hit) {
    throw new Error(`bundle-node-entry: missing required arg --${name}=`);
  }
  return hit.slice(prefix.length);
}

const entry = arg("entry");

// Default: inline everything non-`node:` into a standalone file (the OTel
// preload, loaded with no node_modules sidecar). With --external-bare only the
// entry's own relative (`./`, `../`) imports are inlined and every bare
// specifier stays external — for Nitro plugins that the app's own bundler
// re-processes, where inlining `nitro`/`@opentelemetry/api` would duplicate the
// framework and the OTel global singletons.
const externalBare = process.argv.includes("--external-bare");
const isRelativeOrVirtual = (id: string) =>
  id.startsWith(".") || id.startsWith("/") || id.startsWith("\0");

await build({
  input: entry,
  platform: "node",
  external: (id) =>
    id !== entry && (id.startsWith("node:") || (externalBare && !isRelativeOrVirtual(id))),
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
