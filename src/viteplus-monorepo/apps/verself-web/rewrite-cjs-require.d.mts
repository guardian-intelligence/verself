// Typed face of the generated rewrite-cjs-require.mjs bundle. The runtime is the
// Bazel/Rolldown bundle (viteplus_server_plugin_bundles); the type re-exports
// the workspace source so the declared signature cannot drift from it. vp build
// resolves the .mjs and never reads this .d.mts, so @verself/nitro-plugins stays
// out of the app build graph.
export { rewriteCjsRequireOnCompiled } from "@verself/nitro-plugins/rewrite-cjs-require";
