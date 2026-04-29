// OTel preload. Loaded via Node's `--import` flag in the systemd unit
// (`node --import ./instrumentation.mjs ./.output/server/index.mjs`) so the
// SDK has patched node:http and undici before the Nitro server bundle
// imports either — the same ordering guarantee `--require` once gave to
// CommonJS, expressed as ESM.
import { initOtel } from "@verself/nitro-plugins/otel";

await initOtel("verself-web");
