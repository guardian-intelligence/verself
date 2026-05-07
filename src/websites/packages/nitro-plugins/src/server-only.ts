// Stub resolved by `package.json#exports` for the `default`/`browser`
// condition of the server-only entrypoints. If a client bundle ever pulls
// `@verself/nitro-plugins/observability-plugin` or `/otel`, Vite/Rolldown
// resolves the import to this file and the throw fires the moment the
// module is evaluated — surfacing the boundary leak loudly in browser
// devtools instead of silently shipping nitro/h3 + the OTel SDK in the
// client bundle.
throw new Error(
  "@verself/nitro-plugins: server-only entrypoint imported in a non-Node bundle. " +
    "Move the import behind a server route, a Nitro plugin path, or an instrumentation preload.",
);

export {};
