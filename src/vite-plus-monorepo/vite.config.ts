import { defineConfig } from "vite-plus";

export default defineConfig({
  fmt: {},
  lint: {
    ignorePatterns: ["**/dist/**", "**/.output/**", "**/node_modules/**", "**/routeTree.gen.ts"],
    options: { typeAware: true, typeCheck: true },
  },
  test: {
    include: [
      "apps/**/*.test.ts",
      "apps/**/*.test.tsx",
      "packages/**/*.test.ts",
      "packages/**/*.test.tsx",
    ],
    exclude: ["**/node_modules/**", "**/dist/**", "**/.output/**", "**/e2e/**"],
    environment: "node",
  },
  run: {
    cache: true,
  },
});
