import * as v from "valibot";

// Letters — Guardian's long-form. One .md file per letter under
// ./letters/*.md. Frontmatter declares the metadata; the body is plain
// markdown.
//
// Adding a letter: drop a new .md file in ./letters/ with the frontmatter
// fields below. The Vite plugin (vite.config: company:letters-markdown)
// parses each file at build time; the browser only ever sees pre-rendered
// HTML.

// summary is the only frontmatter field allowed to be absent — that absence
// is the publish gate. A letter with no summary is a draft: it parses, but
// is filtered out of LETTERS below so it does not show up on /letters or
// /letters/$slug. Authors can leave a stub file in ./letters/ while drafting
// without breaking the build, and ship by filling in the summary.
const LetterFrontmatterSchema = v.pipe(
  v.object({
    slug: v.pipe(v.string(), v.minLength(1)),
    title: v.pipe(v.string(), v.minLength(1)),
    // YYYY-MM-DD only. The Vite plugin coerces YAML dates to this shape, so
    // anything else here is an authoring mistake worth surfacing.
    publishedAt: v.pipe(v.string(), v.regex(/^\d{4}-\d{2}-\d{2}$/)),
    flare: v.pipe(v.string(), v.minLength(1)),
    // dispatch: a letter from the author to a younger self — a titled
    // headline, signed by the author. correspondence: a letter received from
    // someone else — it opens with a salutation ("Dear X,") and carries the
    // sender's own sign-off in the body. Required so each letter declares its
    // nature rather than inheriting a silent default.
    kind: v.picklist(["dispatch", "correspondence"]),
    summary: v.optional(v.string(), ""),
  }),
  v.check(
    (fm) => fm.title.includes(fm.flare),
    "flare must be a substring of title — the OG card highlights it",
  ),
);

const LetterModuleSchema = v.object({
  default: v.object({
    frontmatter: LetterFrontmatterSchema,
    html: v.string(),
    leadHtml: v.string(),
    continuationHtml: v.string(),
  }),
});

export type Letter = v.InferOutput<typeof LetterFrontmatterSchema> & {
  readonly bodyHtml: string;
  readonly leadHtml: string;
  readonly continuationHtml: string;
};

export const LETTERS_META = {
  title: "Letters — Guardian",
  description:
    "Long-form from Guardian. Published when we have something to say, not on a calendar.",
  editor: "Guardian",
  siteURL: "https://guardianintelligence.org",
} as const;

function parseLetter(path: string, mod: unknown): Letter {
  const result = v.safeParse(LetterModuleSchema, mod);
  if (!result.success) {
    const issues = result.issues
      .map((i) => `  - ${i.path?.map((p) => String(p.key)).join(".") ?? "<root>"}: ${i.message}`)
      .join("\n");
    throw new Error(`letters: ${path} frontmatter is invalid:\n${issues}`);
  }
  return {
    ...result.output.default.frontmatter,
    bodyHtml: result.output.default.html,
    leadHtml: result.output.default.leadHtml,
    continuationHtml: result.output.default.continuationHtml,
  };
}

const RAW_LETTERS = import.meta.glob<unknown>("./letters/*.md", { eager: true });

export const LETTERS: readonly Letter[] = Object.entries(RAW_LETTERS)
  .map(([path, mod]) => parseLetter(path, mod))
  .filter((letter) => letter.summary.trim() !== "");

export function letterBySlug(slug: string): Letter | undefined {
  return LETTERS.find((letter) => letter.slug === slug);
}

export function sortedLetters(): readonly Letter[] {
  return [...LETTERS].sort((a, b) => (a.publishedAt < b.publishedAt ? 1 : -1));
}
