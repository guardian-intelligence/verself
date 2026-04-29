#!/usr/bin/env node
// Bundles each app's `app-entry.mts` (single entry: OTel preload + dynamic
// import of the Nitro server bundle) into a self-contained `app-entry.mjs`
// that runs without a node_modules sidecar. Uses Rolldown — the same bundler
// `vp build` already runs for the server and client bundles — so resolution,
// exports-condition handling, and CJS/ESM interop match what the rest of the
// app gets.
//
// Why single-entry: the OTel SDK has to patch node:http and undici before
// any traced module is imported. Top-level await on `initOtel(...)` followed
// by `await import("./.output/server/index.mjs")` gives that ordering, and
// systemd's ExecStart shrinks to `node ./app-entry.mjs` instead of carrying
// preload + main as two app-internal facts.
//
// Args:
//   --entry=<path>     app-entry.mts (relative to the cwd Bazel sets via
//                      `chdir = native.package_name()`)
//   --outfile=<path>   app-entry.mjs (sibling of the entry)
import process from "node:process";

import { build } from "rolldown";

function arg(name) {
  const prefix = `--${name}=`;
  const hit = process.argv.find((a) => a.startsWith(prefix));
  if (!hit) {
    throw new Error(`bundle-app-entry: missing required arg --${name}=`);
  }
  return hit.slice(prefix.length);
}

await build({
  input: arg("entry"),
  platform: "node",
  output: {
    file: arg("outfile"),
    format: "esm",
    target: "node22",
    inlineDynamicImports: true,
  },
  // OTel core ships as CJS; bundling it into an ESM output drops the
  // implicit module-scope `require`. Rolldown ships a CJS-in-ESM polyfill,
  // but recreating the real Node `require` from `import.meta.url` is
  // strictly more correct: it lets bundled CJS code call
  // `require('node:util')` etc. exactly as it would in a CJS context.
  banner:
    'import { createRequire as __nodeCreateRequire } from "node:module"; const require = __nodeCreateRequire(import.meta.url);',
  // server.mts uses `await import("./.output/server/index.mjs")` to invoke
  // the Nitro server output at runtime. Keep that runtime resolution — the
  // Nitro bundle isn't on disk at Rolldown bundle time (it's produced by a
  // sibling `viteplus_app` build action), and even if it were, inlining it
  // into server.mjs would defeat the OTel preload-then-server ordering the
  // top-level await provides.
  external: [/\.output\/.*/],
});
