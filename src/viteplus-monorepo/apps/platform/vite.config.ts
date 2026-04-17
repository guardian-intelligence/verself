import { fileURLToPath } from "node:url";
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import { nitro } from "nitro/vite";
import { defineConfig } from "vite-plus";

const observabilityPlugin = fileURLToPath(
  import.meta.resolve("@forge-metal/nitro-plugins/observability-plugin"),
);

// The canonical policy source lives outside this package at
// src/platform/policies/; authorize Vite's dev-server FS gate to read it so
// policy-catalog.ts can ?raw-import the YAML directly instead of maintaining a
// generated copy that can silently drift.
const policiesDir = path.resolve(import.meta.dirname, "../../../platform/policies");

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: 4249,
    strictPort: true,
    fs: { allow: [".", policiesDir] },
  },
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [
    tailwindcss(),
    tanstackStart({ srcDirectory: "src" }),
    viteReact(),
    nitro({ plugins: [observabilityPlugin] }),
  ],
  test: {
    exclude: ["**/node_modules/**", "**/e2e/**", "**/*.spec.ts"],
    passWithNoTests: true,
  },
});
