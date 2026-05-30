import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import { nitro } from "nitro/vite";
import { defineConfig } from "vite-plus";
import { dotMatrixShaderGeneration } from "./dot-matrix-shader-plugin";
// Bazel/Rolldown-bundled forms of the @verself/nitro-plugins entrypoints (see
// viteplus_server_plugin_bundles). Importing the local .mjs keeps the workspace
// package's .ts source out of the vp build module graph.
import { rewriteCjsRequireOnCompiled } from "./rewrite-cjs-require.mjs";

const observabilityPlugin = fileURLToPath(new URL("./observability-plugin.mjs", import.meta.url));
const localAppPort = Number.parseInt(process.env.VERSELF_WEB_DEV_LOCAL_APP_PORT ?? "4244", 10);

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number.isNaN(localAppPort) ? 4244 : localAppPort,
    strictPort: true,
  },
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [
    dotMatrixShaderGeneration(),
    tailwindcss(),
    tanstackStart({ srcDirectory: "src" }),
    viteReact(),
    nitro({
      plugins: [observabilityPlugin],
      hooks: { compiled: rewriteCjsRequireOnCompiled },
    }),
  ],
  test: {
    exclude: ["**/node_modules/**"],
    passWithNoTests: true,
  },
});
