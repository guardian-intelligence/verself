import { defineConfig } from "vite-plus";

export default defineConfig({
  pack: {
    dts: {
      // Keep package builds reproducible on clean deploy hosts.
      tsgo: false,
    },
    exports: true,
  },
  lint: {
    ignorePatterns: ["dist/**"],
    options: {
      typeAware: true,
      typeCheck: true,
    },
  },
  fmt: {},
});
