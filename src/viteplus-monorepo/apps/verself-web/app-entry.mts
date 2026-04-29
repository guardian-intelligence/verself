// Single app entry point. Top-level await on the OTel preload guarantees the
// SDK has patched node:http and undici before the dynamic import below
// imports the Nitro server bundle, which is the same ordering guarantee
// `node --import preload main` provides — just expressed in JS so systemd's
// ExecStart shrinks to `node ./app-entry.mjs` instead of carrying preload +
// main as two app-internal facts.
import { initOtel } from "@verself/nitro-plugins/otel";

await initOtel("verself-web");

// `vp build` (Vite/Rolldown) scans .mts files at the project root and would
// try to statically resolve this import — but `.output/` is produced by a
// sibling Bazel action, not by Vite, so the path doesn't exist at vp-build
// time. The /* @vite-ignore */ comment tells Vite to leave this dynamic
// import alone; resolution happens at runtime inside the deploy tarball
// where `.output/server/index.mjs` is a real file. TS likewise has no
// declarations for the not-yet-built Nitro output; @ts-expect-error so the
// typecheck fails loudly if a future build setup makes the path resolvable
// and the suppression silently stops applying.
// @ts-expect-error -- runtime-only path, see comment above
await import(/* @vite-ignore */ "./.output/server/index.mjs");
