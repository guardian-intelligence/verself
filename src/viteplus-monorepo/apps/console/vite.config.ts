import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import { nitro } from "nitro/vite";
import { defineConfig } from "vite-plus";

const observabilityPlugin = fileURLToPath(
  import.meta.resolve("@verself/nitro-plugins/observability-plugin"),
);
const localAppPort = Number.parseInt(process.env.CONSOLE_DEV_LOCAL_APP_PORT ?? "4244", 10);

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
