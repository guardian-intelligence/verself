import { createFileRoute, Link } from "@tanstack/react-router";
import { LETTERS_META, sortedLetters, type Letter } from "~/content/letters";
import { ogMeta } from "~/lib/head";

// Letters index. A diary, not a periodical. Each entry is a date — no
// titles, no summaries, no rules. Titles live in frontmatter for SEO / OG /
// browser tab only; they never render here. The chrome already says
// "Guardian · Letters", so the page opens directly into the column.

export const Route = createFileRoute("/letters/")({
  component: LettersIndex,
  head: () => ({
    meta: ogMeta({
      slug: "letters",
      title: LETTERS_META.title,
      description: LETTERS_META.description,
    }),
  }),
});

function formatDate(iso: string): string {
  const d = new Date(`${iso}T12:00:00Z`);
  return d.toLocaleDateString("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
    timeZone: "UTC",
  });
}

function LettersIndex() {
  const letters = sortedLetters();

  return (
    <div className="mx-auto w-full max-w-[60rem] px-5 py-16 sm:px-8 sm:py-24 md:px-10 md:py-32">
      <ul className="flex flex-col gap-10 sm:gap-12 md:gap-14">
        {letters.map((letter) => (
          <li key={letter.slug}>
            <DateEntry letter={letter} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function DateEntry({ letter }: { letter: Letter }) {
  return (
    <Link
      to="/letters/$slug"
      params={{ slug: letter.slug }}
      className="inline-block text-[19px] leading-[1.4] sm:text-[20px] font-display italic text-[var(--treatment-ink)] no-underline hover:underline hover:decoration-[var(--treatment-rule-color)] hover:underline-offset-[0.18em] [font-variation-settings:'opsz'_24,'SOFT'_30]"
    >
      {formatDate(letter.publishedAt)}
    </Link>
  );
}
