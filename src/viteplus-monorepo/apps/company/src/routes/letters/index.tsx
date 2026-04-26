import { createFileRoute, Link } from "@tanstack/react-router";
import { LETTERS_META, sortedLetters, type Letter } from "~/content/letters";
import { ogMeta } from "~/lib/head";

// Letters index. The page reads as a periodical front page without the
// nameplate cosplay (no "Letters" giant header, no Vol/Issue line, no
// dateline mast — the chrome already says "Guardian · Letters", repeating
// it below would be ceremony). The lead story sits centered at the top of
// the page; subsequent letters fall into a two-column reading band beneath
// a single sepia rule. Bylines drop the "· Guardian" suffix because the
// site IS Guardian — repeating it on every card is the same ceremony.

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
  const [lead, ...rest] = letters;

  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-20 md:px-6 md:py-28">
      {lead ? <LeadStory letter={lead} /> : null}
      {rest.length > 0 ? <StoryGrid letters={rest} /> : null}
    </div>
  );
}

function LeadStory({ letter }: { letter: Letter }) {
  // The lead is centered with generous vertical air around the headline.
  // Fraunces at display scale carries the page; the deck and dateline sit
  // beneath the headline in narrower measures. The whole composition is
  // one click target.
  return (
    <section>
      <Link
        to="/letters/$slug"
        params={{ slug: letter.slug }}
        className="group block text-center"
        style={{ color: "var(--treatment-ink)" }}
      >
        <h2
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 144, "SOFT" 30',
            fontWeight: 400,
            fontSize: "clamp(44px, 7vw, 80px)",
            lineHeight: 1.02,
            letterSpacing: "-0.024em",
            margin: 0,
          }}
        >
          {letter.title}
        </h2>
        <p
          className="mx-auto mt-10"
          style={{
            fontFamily: "var(--treatment-body-font)",
            fontVariationSettings: '"opsz" 18, "SOFT" 0',
            fontSize: "clamp(18px, 1.8vw, 21px)",
            lineHeight: 1.5,
            color: "var(--treatment-muted-strong)",
            margin: "40px auto 0",
            maxWidth: "56ch",
          }}
        >
          {letter.summary}
        </p>
        <p
          className="mt-8 font-mono text-[10px] uppercase tracking-[0.18em]"
          style={{ color: "var(--treatment-muted-meta)" }}
        >
          {formatDate(letter.publishedAt)}
        </p>
      </Link>
    </section>
  );
}

function StoryGrid({ letters }: { letters: readonly Letter[] }) {
  // The continuation. A single sepia rule above the band marks the break
  // from the lead; below it, headlines fall into two columns at md+ and
  // stack on mobile. Each story sits on its own without a kicker tag —
  // the title and deck do the work.
  return (
    <section
      className="mt-20 grid gap-x-10 gap-y-14 pt-12 md:grid-cols-2 md:mt-28 md:pt-16"
      style={{
        borderTop:
          "var(--treatment-rule-thickness) solid var(--treatment-rule-color)",
      }}
    >
      {letters.map((letter) => (
        <article key={letter.slug}>
          <Link
            to="/letters/$slug"
            params={{ slug: letter.slug }}
            className="group block"
            style={{ color: "var(--treatment-ink)" }}
          >
            <h3
              style={{
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 60, "SOFT" 30',
                fontWeight: 400,
                fontSize: "clamp(26px, 2.8vw, 32px)",
                lineHeight: 1.1,
                letterSpacing: "-0.018em",
                margin: 0,
              }}
            >
              {letter.title}
            </h3>
            <p
              className="mt-4"
              style={{
                fontFamily: "var(--treatment-body-font)",
                fontVariationSettings: '"opsz" 18, "SOFT" 0',
                fontSize: "16px",
                lineHeight: 1.55,
                color: "var(--treatment-muted-strong)",
                margin: 0,
              }}
            >
              {letter.summary}
            </p>
            <p
              className="mt-4 font-mono text-[10px] uppercase tracking-[0.18em]"
              style={{ color: "var(--treatment-muted-meta)" }}
            >
              {formatDate(letter.publishedAt)}
            </p>
          </Link>
        </article>
      ))}
    </section>
  );
}
