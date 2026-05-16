import { createFileRoute, Link } from "@tanstack/react-router";
import { LETTERS_META, sortedLetters, type Letter } from "~/content/letters";
import { ogMeta } from "~/lib/head";

// Letters index. Each entry is a piece of correspondence on the page: a
// small dated line, the title in the hand, and the opening of the letter
// itself dissolving back into the paper a few lines in — the reader sees
// enough to know whether to open it, never a summary written *about* it.
// The excerpt is the letter's real first words (the frontmatter summary is
// SEO/OG only and never renders). A letter with an empty body is one that
// has been dated and titled but not yet written; it shows the title alone
// rather than a faked preview.

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

function ordinal(n: number): string {
  const teens = n % 100;
  if (teens >= 11 && teens <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

// Same ordinal voice as the letter page ("July 4th, 2036"), so the date a
// reader sees in the list is the date the letter opens with.
function formatDate(iso: string): string {
  const d = new Date(`${iso}T12:00:00Z`);
  const month = d.toLocaleDateString("en-US", { month: "long", timeZone: "UTC" });
  return `${month} ${ordinal(d.getUTCDate())}, ${d.getUTCFullYear()}`;
}

// The letter's opening, in plain text. marked emits block HTML with the
// HTML-special characters entity-escaped; strip the tags, turn block
// boundaries into spaces, and decode the five entities marked produces
// (&amp; last so an author-typed `&lt;` survives as text). Capped on a word
// boundary so a long letter never ships its whole body into the list DOM —
// the visual fade, not an ellipsis, signals there is more.
function excerptOf(bodyHtml: string): string {
  const text = bodyHtml
    .replace(/<\/(p|h[1-6]|li|blockquote|pre)>/gi, " ")
    .replace(/<[^>]+>/g, "")
    .replace(/&#39;/g, "'")
    .replace(/&quot;/g, '"')
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&amp;/g, "&")
    .replace(/\s+/g, " ")
    .trim();
  if (text.length <= 280) return text;
  const cut = text.slice(0, 280);
  const lastSpace = cut.lastIndexOf(" ");
  return lastSpace > 200 ? cut.slice(0, lastSpace) : cut;
}

function LettersIndex() {
  const letters = sortedLetters();

  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-16 sm:py-24 md:px-6 md:py-32">
      <ul className="flex flex-col gap-12 sm:gap-14 md:gap-16">
        {letters.map((letter) => (
          <li key={letter.slug}>
            <LetterEntry letter={letter} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function LetterEntry({ letter }: { letter: Letter }) {
  const excerpt = excerptOf(letter.bodyHtml);

  return (
    <Link to="/letters/$slug" params={{ slug: letter.slug }} className="group block no-underline">
      <p
        className="font-display italic text-[var(--treatment-muted-meta)] [font-variation-settings:'opsz'_18,'SOFT'_30]"
        style={{ fontSize: "14px", lineHeight: 1.4, margin: 0 }}
      >
        {formatDate(letter.publishedAt)}
      </p>
      <h2
        className="mt-[0.35em] font-display font-normal text-[var(--treatment-ink)] leading-[1.16] [font-variation-settings:'opsz'_24,'SOFT'_0] text-[clamp(22px,2.4vw,28px)] decoration-[var(--treatment-rule-color)] underline-offset-[0.18em] group-hover:underline"
        style={{ margin: 0 }}
      >
        {letter.title}
      </h2>
      {excerpt ? (
        <p
          aria-hidden
          className="mt-[0.7em] font-display font-normal text-[var(--treatment-muted-strong)] [font-variation-settings:'opsz'_18,'SOFT'_0] text-[16px] md:text-[17px]"
          style={{
            margin: 0,
            lineHeight: 1.62,
            maxHeight: "calc(1.62em * 3)",
            overflow: "hidden",
            WebkitMaskImage: "linear-gradient(to bottom, #000 0 78%, transparent 100%)",
            maskImage: "linear-gradient(to bottom, #000 0 78%, transparent 100%)",
          }}
        >
          {excerpt}
        </p>
      ) : null}
    </Link>
  );
}
