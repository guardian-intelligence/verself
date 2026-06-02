import { createFileRoute, Link, notFound, useSearch } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useEffect } from "react";
import { letterBySlug, type Letter } from "~/content/letters";
import { HandwrittenSignature } from "~/features/letters/handwritten-signature";
import {
  LETTER_POST_PAGE_PADDING_CLASS,
  LETTER_READING_COLUMN_CLASS,
  LETTER_TEXT_MEASURE_CLASS,
  LetterBody,
  LetterDate,
  LetterSalutation,
} from "~/features/letters/typography";
import {
  LETTER_RETURN_VIEW_TRANSITION,
  letterNavigationIntentHandlers,
} from "~/features/letters/transitions.intent";
import {
  fixedTransitionStyle,
  LETTER_RETURN_TRANSITION_NAME,
} from "~/features/letters/transitions";
import { LetterOgPreview, LetterOgPreviewHotkey } from "~/features/letters/og-preview";
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
  const search = useSearch({ strict: false }) as {
    readonly developmentMode?: string | number;
    readonly og?: string | number;
  };
  // Developer affordance: Cmd+D arms developer mode, then "s" swaps the article
  // for the OG card (PNG + source SVG). The article isn't rendered while on.
  // (TanStack parses ?og=1 to the number 1, so coerce before comparing.)
  const showOgPreview = String(search.developmentMode) === "1" && String(search.og) === "1";

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
      <LetterOgPreviewHotkey />
      {showOgPreview ? (
        <LetterOgPreview slug={letter.slug} />
      ) : (
        <div className={LETTER_TEXT_MEASURE_CLASS} data-letter-entry={letter.slug}>
          <LetterReturnLink />
          <LetterDate letter={letter} />
          <LetterSalutation letter={letter} />
          <LetterBody letter={letter} />
          <LetterSignature letter={letter} />
        </div>
      )}
    </article>
  );
}

function LetterReturnLink() {
  return (
    <Link
      to="/letters"
      activeOptions={{ exact: true }}
      viewTransition={LETTER_RETURN_VIEW_TRANSITION}
      {...letterNavigationIntentHandlers("return")}
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

// The sign-off, by letter kind. A dispatch — a letter from the author to a
// younger self — closes with his hand:
// The dispatch sign-off is a rectified trace of the author's ink signature.
// The SVG carries the accessible name so the close is readable and indexable.
//
// Correspondence (dear-shovon, written by someone else) is signed by a hand we
// don't reveal: a redaction bar struck in the letter's ink where the name
// would be — twice the footprint of the name it covers, a deliberate strike.
// The body already carries the sender's own 💙 close just above it. The faint
// paper grid multiplies over the bar so it reads as a marker stroke on the
// sheet; the slight rotation keeps it a drawn gesture, not a sterile box.
function LetterSignature({ letter }: { readonly letter: Letter }) {
  if (letter.kind === "dispatch") {
    return (
      <div className="mt-16" style={{ marginLeft: "72px" }}>
        <HandwrittenSignature className="block h-auto w-[min(100%,clamp(180px,25vw,252px))] text-[var(--treatment-ink)]" />
        <p
          aria-label="By Shovon Hasan - Founder, Guardian Intelligence Inc. Est. 2026 Seattle, WA"
          className="mt-3 max-w-[min(100%,clamp(180px,25vw,252px))] text-right font-display text-[clamp(10.5px,0.8vw,12px)] leading-[1.35] tracking-[0.02em] text-[var(--treatment-muted-strong)]"
        >
          <span className="block text-[var(--treatment-ink)]">By Shovon Hasan</span>{" "}
          <span className="block">Founder, Guardian Intelligence Inc.</span>{" "}
          <span className="block">Est. 2026 · Seattle, WA</span>
        </p>
      </div>
    );
  }
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
