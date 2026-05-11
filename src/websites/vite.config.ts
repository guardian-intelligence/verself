import { defineConfig } from "vite-plus";

export default defineConfig({
  fmt: {
    ignorePatterns: ["**/__generated/**", "**/routeTree.gen.ts"],
  },
  lint: {
    ignorePatterns: [
      "**/dist/**",
      "**/.output/**",
      "**/node_modules/**",
      "**/__generated/**",
      "**/routeTree.gen.ts",
    ],
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
    exclude: ["**/node_modules/**", "**/dist/**", "**/.output/**"],
    environment: "node",
  },
  run: {
    cache: true,
  },
});
