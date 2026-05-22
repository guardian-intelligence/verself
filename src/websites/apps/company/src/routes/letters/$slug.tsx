import { createFileRoute, Link, notFound } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useEffect } from "react";
import { letterBySlug } from "~/content/letters";
import {
  LETTER_POST_PAGE_PADDING_CLASS,
  LETTER_READING_COLUMN_CLASS,
  LETTER_TEXT_MEASURE_CLASS,
  LetterBody,
  LetterDate,
  LetterSalutation,
} from "~/features/letters/typography";
import {
  fixedTransitionStyle,
  LETTER_RETURN_TRANSITION_NAME,
} from "~/features/letters/transitions";
import { ogMeta } from "~/lib/head";
import { emitSpan } from "~/lib/telemetry/browser";

// A single letter. The form follows DESIGN.md: the date sits at the very
// top, left-aligned to the column, sized to exactly two graph-paper cells
// (2 × 28px) the way it was always written by hand. The title is the
// salutation — "Dear Shovon," — and renders directly under the date, in the
// body's hand, the way a letter is actually addressed. The body opens
// underneath on the same measure as the index preview, so browser view
// transitions have stable type geometry to interpolate. The body closes on
// "Love,"; the name is not signed but redacted — a solid marker bar, four
// characters wide, struck where it would be. The frontmatter title doubles as
// the <head>/OG title.

export const Route = createFileRoute("/letters/$slug")({
  component: LetterPost,
  loader: ({ params }) => {
    const letter = letterBySlug(params.slug);
    if (!letter) {
      throw notFound();
    }
    return { letter };
  },
  head: ({ loaderData }) => {
    const letter = loaderData?.letter;
    if (!letter) {
      return { meta: [{ title: "Letters — Guardian" }] };
    }
    return {
      meta: ogMeta({
        slug: `letter/${letter.slug}`,
        title: `${letter.title} — Guardian`,
        description: letter.summary,
      }),
    };
  },
});

function LetterPost() {
  const { letter } = Route.useLoaderData();

  useEffect(() => {
    emitSpan("company.letter.view", {
      "letter.slug": letter.slug,
      "letter.published_at": letter.publishedAt,
    });
  }, [letter.slug, letter.publishedAt]);

  return (
    <article
      data-letter-transition-route="post"
      className={`${LETTER_READING_COLUMN_CLASS} ${LETTER_POST_PAGE_PADDING_CLASS}`}
    >
      <div className={LETTER_TEXT_MEASURE_CLASS} data-letter-entry={letter.slug}>
        <LetterReturnLink />
        <LetterDate letter={letter} />
        <LetterSalutation letter={letter} />
        <LetterBody letter={letter} />
        <RedactionMark />
      </div>
    </article>
  );
}

function LetterReturnLink() {
  return (
    <Link
      to="/letters"
      activeOptions={{ exact: true }}
      data-letter-return
      aria-label="Return to letters"
      className="-mx-3 mb-2 inline-flex min-h-11 items-center gap-2 px-3 font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--treatment-muted-meta)] no-underline outline-none focus-visible:ring-2 focus-visible:ring-[var(--treatment-rule-color)] focus-visible:ring-offset-4 focus-visible:ring-offset-[var(--treatment-ground)]"
      style={fixedTransitionStyle(LETTER_RETURN_TRANSITION_NAME)}
    >
      <ArrowLeft aria-hidden="true" size={13} strokeWidth={1.75} />
      <span>Return</span>
    </Link>
  );
}

// The signature, redacted. A solid marker bar in the letter's own ink —
// twice the footprint of the four-character name it covers (double width,
// double the line's height): a deliberate strike, not a tight cover. It
// sits tight under "Love," so the two read as one sign-off block, not a
// closing and a stray rectangle. Decorative only: aria-hidden,
// no text, nothing for a screen reader or crawler to read. The faint paper
// grid still multiplies over it, so it reads as a marker stroke on the
// sheet. The slight rotation and softened ends keep it a drawn gesture,
// deterministic (no random), not a sterile box.
function RedactionMark() {
  return (
    <div
      aria-hidden
      className="font-display text-[clamp(20px,1.6vw,22px)]"
      style={{ marginTop: "8px" }}
    >
      <span
        style={{
          display: "inline-block",
          width: "8ch",
          height: "1.64em",
          background: "var(--treatment-ink)",
          borderRadius: "2px",
          transform: "rotate(-0.5deg)",
        }}
      />
    </div>
  );
}
