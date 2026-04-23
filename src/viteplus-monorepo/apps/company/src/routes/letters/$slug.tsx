import { createFileRoute, notFound } from "@tanstack/react-router";
import { useEffect } from "react";
import { letterBySlug } from "~/content/letters";
import { BodyParagraph, PageShell } from "~/components/page-shell";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// Individual letter. Editorial → Paper ground, consistent with /letters/.
// All letters share /og/letters for their social card until per-letter cards
// are warranted; the description is the letter summary so the unfurl text is
// distinct per post.

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
        slug: "letters",
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
    <PageShell
      kicker={`${letter.publishedAt} · ${letter.kicker}`}
      heading={letter.title}
    >
      {letter.body.map((paragraph, idx) => (
        <BodyParagraph key={idx}>{paragraph}</BodyParagraph>
      ))}
      <p
        className="mt-8 font-mono text-[10px] uppercase tracking-[0.18em]"
        style={{ color: "var(--treatment-muted-meta)" }}
      >
        Signed · {letter.author} · Seattle, Washington
      </p>
    </PageShell>
  );
}
