import { createFileRoute, notFound } from "@tanstack/react-router";
import { useEffect } from "react";
import { letterBySlug } from "~/content/letters";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// A single letter. The form follows DESIGN.md: the date sits at the very
// top, left-aligned to the column, sized to exactly two graph-paper cells
// (2 × 28px) the way it was always written by hand. The title is the
// salutation — "Dear Shovon" — and renders directly under the date, in the
// body's hand, the way a letter is actually addressed. The body opens
// underneath and runs the full width of the column — the same frame as the
// masthead, no narrow measure — because these were written to the edge of
// the page. The body closes on "Love,"; the name is not signed but redacted
// — a solid marker bar, four characters wide, struck where it would be. The
// frontmatter title doubles as the <head>/OG title.

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

// "July 4th, 2036" — the date in the hand it was written in, ordinal and
// all, not the calendar's "July 4, 2036". The date is the one thing every
// letter opens with, so it carries the voice.
function formatDate(iso: string): string {
  const d = new Date(`${iso}T12:00:00Z`);
  const month = d.toLocaleDateString("en-US", { month: "long", timeZone: "UTC" });
  return `${month} ${ordinal(d.getUTCDate())}, ${d.getUTCFullYear()}`;
}

const letterProseClassName = [
  "w-full font-display text-[var(--treatment-muted-strong)]",
  "[font-variation-settings:'opsz'_18,'SOFT'_0]",
  "[overflow-wrap:break-word]",
  "[&>*+*]:mt-7",
  "[&>p]:text-[18px] [&>p]:font-normal [&>p]:leading-[1.62] md:[&>p]:text-[clamp(19px,1.4vw,20px)]",
  "[&>blockquote]:border-l-2 [&>blockquote]:border-[var(--treatment-rule-color)] [&>blockquote]:pl-5 [&>blockquote]:italic",
  "[&>blockquote]:text-[18px] [&>blockquote]:leading-[1.62] md:[&>blockquote]:text-[clamp(19px,1.4vw,20px)]",
  "[&>ul]:list-disc [&>ol]:list-decimal [&>ul]:pl-7 [&>ol]:pl-7",
  "[&_li]:mt-2 [&_li]:text-[18px] [&_li]:leading-[1.62] md:[&_li]:text-[clamp(19px,1.4vw,20px)]",
  "[&_a]:text-[var(--treatment-ink)] [&_a]:underline [&_a]:decoration-[1px] [&_a]:underline-offset-[0.18em]",
  "[&>h2]:mt-14 [&>h2]:font-display [&>h2]:text-[clamp(24px,2.4vw,30px)] [&>h2]:font-normal [&>h2]:leading-[1.18]",
  "[&>h3]:mt-12 [&>h3]:font-display [&>h3]:text-[clamp(20px,2vw,24px)] [&>h3]:font-normal [&>h3]:leading-[1.22]",
].join(" ");

function LetterPost() {
  const { letter } = Route.useLoaderData();

  useEffect(() => {
    emitSpan("company.letter.view", {
      "letter.slug": letter.slug,
      "letter.published_at": letter.publishedAt,
    });
  }, [letter.slug, letter.publishedAt]);

  return (
    <article className="mx-auto w-full max-w-6xl px-4 pb-24 pt-16 md:px-6 md:pb-32 md:pt-20">
      <p
        className="font-display italic text-[var(--treatment-ink)] [font-variation-settings:'opsz'_60,'SOFT'_30]"
        // Exactly two graph cells tall (2 × 28px = 56px line box), left
        // on the column. The face is large but the line box is locked to
        // the ruling so the date rests on the grid like the hand-written
        // original.
        style={{ fontSize: "44px", lineHeight: "56px", margin: 0 }}
      >
        {formatDate(letter.publishedAt)}
      </p>
      <p
        className="font-display font-normal text-[var(--treatment-ink)] [font-variation-settings:'opsz'_18,'SOFT'_0]"
        // The salutation, in the body's hand (not the date's italic voice).
        // Sits one cell under the date; the letter opens one cell below it.
        // The comma is presentational — the frontmatter title stays the
        // clean "Dear Shovon" for <head>/OG.
        style={{
          margin: 0,
          marginTop: "28px",
          fontSize: "clamp(20px,1.6vw,22px)",
          lineHeight: 1.4,
        }}
      >
        {letter.title},
      </p>
      <div
        className={letterProseClassName}
        style={{ marginTop: "28px" }}
        // The body is markdown rendered to HTML at build time by the
        // company:letters-markdown Vite plugin; Tailwind child selectors
        // keep the correspondence typography local to this route.
        dangerouslySetInnerHTML={{ __html: letter.bodyHtml }}
      />
      <RedactionMark />
    </article>
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
