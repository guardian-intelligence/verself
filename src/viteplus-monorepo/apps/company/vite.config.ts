import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import { rewriteCjsRequireOnCompiled } from "@verself/nitro-plugins/rewrite-cjs-require";
import matter from "gray-matter";
import { marked } from "marked";
import { nitro } from "nitro/vite";
import { defineConfig } from "vite-plus";

const observabilityPlugin = fileURLToPath(
  import.meta.resolve("@verself/nitro-plugins/observability-plugin"),
);

// Letters markdown loader. Each src/content/letters/*.md becomes a JS module
// exporting { frontmatter, html, leadHtml, continuationHtml } parsed at build
// time. Keeps the markdown parser out of the browser bundle entirely — the
// runtime only sees the pre-rendered HTML, so client navigation between
// letters is a static asset hop with no parse cost.
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
    const tokens = marked.lexer(content);
    const flowTokens = tokens.filter((token) => token.type !== "space");
    const [leadToken, ...continuationTokens] = flowTokens;
    const html = marked.parser(tokens);
    const leadHtml = leadToken ? marked.parser([leadToken]) : "";
    const continuationHtml = continuationTokens.length > 0 ? marked.parser(continuationTokens) : "";
    return `export default ${JSON.stringify({
      frontmatter: normalised,
      html,
      leadHtml,
      continuationHtml,
    })};`;
  },
};

// OG-card fonts, baked into the server bundle as base64 at build time. The OG
// route rasterises its SVG to PNG with resvg (social platforms reject SVG
// cards), and resvg needs the actual font bytes — there is no system Fraunces.
// Inlining sidesteps any runtime path/cwd assumptions in the deployed
// artifact: the bytes travel inside the JS, identical in dev and prod.
const OG_FONTS_ID = "virtual:og-fonts";
const ogFonts = {
  name: "company:og-fonts",
  resolveId(id: string) {
    if (id === OG_FONTS_ID) return `\0${OG_FONTS_ID}`;
    return null;
  },
  load(id: string) {
    if (id !== `\0${OG_FONTS_ID}`) return null;
    const dir = fileURLToPath(new URL("./public/fonts", import.meta.url));
    const b64 = (file: string) => readFileSync(`${dir}/${file}`).toString("base64");
    return [
      `export const FRAUNCES_WOFF2_B64 = ${JSON.stringify(b64("Fraunces-Variable.woff2"))};`,
      `export const GEIST_WOFF2_B64 = ${JSON.stringify(b64("Geist-Variable.woff2"))};`,
    ].join("\n");
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
  // @resvg/resvg-js is a native napi addon used only by the server-side OG
  // route. It must never be pre-bundled (the optimizer reads its .node binary
  // as UTF-8 and fails) or bundled into the SSR graph — keep it external so it
  // loads via require() from node_modules at runtime.
  optimizeDeps: {
    exclude: ["@resvg/resvg-js"],
  },
  ssr: {
    external: ["@resvg/resvg-js"],
  },
  plugins: [
    lettersMarkdown,
    ogFonts,
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
