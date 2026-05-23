import { defineConfig } from "vite-plus";

const generatedIgnorePatterns = [
  "**/__generated/**",
  "**/__generated_sources/**",
  "**/routeTree.gen.ts",
] as const;

const buildOutputIgnorePatterns = [
  "**/dist/**",
  "**/dist-ssr/**",
  "**/.output/**",
  "**/.tanstack/**",
  "**/.vinxi/**",
  "**/coverage/**",
  "**/test-results/**",
  "**/playwright-report/**",
  "**/blob-report/**",
] as const;

const toolIgnorePatterns = [
  "**/node_modules/**",
  ...buildOutputIgnorePatterns,
  ...generatedIgnorePatterns,
] as const;

export default defineConfig({
  fmt: {
    ignorePatterns: [...toolIgnorePatterns],
  },
  lint: {
    ignorePatterns: [...toolIgnorePatterns],
    options: { typeAware: true, typeCheck: true },
    rules: {
      "no-console": ["error", { allow: ["error"] }],
    },
    overrides: [
      {
        files: ["packages/nitro-plugins/**", "scripts/**"],
        rules: {
          "no-console": "off",
        },
      },
    ],
  },
  test: {
    include: [
      "apps/**/*.test.ts",
      "apps/**/*.test.tsx",
      "packages/**/*.test.ts",
      "packages/**/*.test.tsx",
    ],
    exclude: ["**/node_modules/**", ...buildOutputIgnorePatterns],
    environment: "node",
  },
  run: {
    cache: true,
  },
});
