import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import matter from "gray-matter";
import { marked } from "marked";
import { nitro } from "nitro/vite";
import { defineConfig } from "vite-plus";

const observabilityPlugin = fileURLToPath(
  import.meta.resolve("@verself/nitro-plugins/observability-plugin"),
);

// Letters markdown loader. Each src/content/letters/*.md becomes a JS module
// exporting { frontmatter, html } parsed at build time. Keeps the markdown
// parser out of the browser bundle entirely — the runtime only sees the
// pre-rendered HTML, so client navigation between letters is a static asset
// hop with no parse cost.
const LETTERS_MD = /\/src\/content\/letters\/[^/]+\.md$/;
const lettersMarkdown = {
  name: "company:letters-markdown",
  enforce: "pre" as const,
  load(id: string) {
    if (!LETTERS_MD.test(id)) return null;
    const raw = readFileSync(id, "utf8");
    const { data, content } = matter(raw);
    // gray-matter parses unquoted YAML dates (publishedAt: 2026-04-08) into
    // JS Dates. JSON.stringify would then emit a full ISO datetime, which
    // breaks the YYYY-MM-DD contract Letter consumers expect. Walk the
    // frontmatter and flatten any Date to a date-only string before serialise.
    const normalised: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(data)) {
      normalised[key] = value instanceof Date ? value.toISOString().slice(0, 10) : value;
    }
    const html = marked.parse(content, { async: false }) as string;
    return `export default ${JSON.stringify({ frontmatter: normalised, html })};`;
  },
};

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: 4252,
    strictPort: true,
  },
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [
    lettersMarkdown,
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
