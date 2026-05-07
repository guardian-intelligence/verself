import { createFileRoute, notFound } from "@tanstack/react-router";
import { useEffect } from "react";
import { letterBySlug } from "~/content/letters";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// Individual letter. Editorial → Paper ground, consistent with /letters/.
// The detail page renders without PageShell so the title can sit centered
// and the dateline drops the kicker tag — the page is the letter, not a
// section of a larger document, so the kicker has nowhere to point.

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

function formatDate(iso: string): string {
  const d = new Date(`${iso}T12:00:00Z`);
  return d.toLocaleDateString("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
    timeZone: "UTC",
  });
}

function LetterPost() {
  const { letter } = Route.useLoaderData();

  useEffect(() => {
    emitSpan("company.letter.view", {
      "letter.slug": letter.slug,
      "letter.published_at": letter.publishedAt,
    });
  }, [letter.slug, letter.publishedAt]);

  return (
    <article className="mx-auto w-full max-w-6xl px-4 py-20 md:px-6 md:py-28">
      <header className="text-center">
        <h1
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 144, "SOFT" 30',
            fontWeight: 400,
            fontSize: "clamp(44px, 7vw, 80px)",
            lineHeight: 1.02,
            letterSpacing: "-0.024em",
            color: "var(--treatment-ink)",
            margin: 0,
          }}
        >
          {letter.title}
        </h1>
        <p
          className="mt-8 font-mono text-[10px] uppercase tracking-[0.18em]"
          style={{ color: "var(--treatment-muted-meta)" }}
        >
          {formatDate(letter.publishedAt)}
        </p>
      </header>
      <div
        className="letter-prose mx-auto mt-16 md:mt-20"
        style={{ maxWidth: "62ch" }}
        // The body is markdown rendered to HTML at build time by the
        // company:letters-markdown Vite plugin; per-element typography is
        // applied via the .letter-prose CSS scope.
        dangerouslySetInnerHTML={{ __html: letter.bodyHtml }}
      />
    </article>
  );
}
