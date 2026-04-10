import { defineConfig } from "vite-plus";

export default defineConfig({
  fmt: {
    ignorePatterns: ["**/routeTree.gen.ts"],
  },
  lint: {
    ignorePatterns: [
      "**/dist/**",
      "**/.output/**",
      "**/node_modules/**",
      "**/__generated/**",
      "**/routeTree.gen.ts",
      "**/e2e/**",
      "**/playwright.config.ts",
    ],
    options: { typeAware: true, typeCheck: true },
  },
  test: {
    include: [
      "apps/**/*.test.ts",
      "apps/**/*.test.tsx",
      "packages/**/*.test.ts",
      "packages/**/*.test.tsx",
    ],
    exclude: ["**/node_modules/**", "**/dist/**", "**/.output/**", "**/e2e/**", "**/*.spec.ts"],
    environment: "node",
  },
  run: {
    cache: true,
  },
});
