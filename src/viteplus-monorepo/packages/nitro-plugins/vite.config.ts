import { defineConfig } from "vite-plus";

export default defineConfig({
  pack: {
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
