import { createFileRoute, notFound } from "@tanstack/react-router";
import { useEffect } from "react";
import { letterBySlug } from "~/content/letters";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// Individual letter. Diary form: a date in the upper-right of the column,
// the body opening directly underneath, signed at the foot. The frontmatter
// title flows into <head> for SEO / OG / browser tab; it never renders.

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

const letterProseClassName = [
  "mt-12 w-full max-w-full font-display text-[var(--treatment-muted-strong)]",
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
    <article className="mx-auto w-full max-w-[60rem] px-6 pb-24 pt-16 sm:px-8 md:px-10 md:pb-32 md:pt-24">
      <header className="flex justify-end">
        <p
          className="font-display italic text-[var(--treatment-muted-strong)] [font-variation-settings:'opsz'_18,'SOFT'_30]"
          style={{ fontSize: "15px", lineHeight: 1.4 }}
        >
          {formatDate(letter.publishedAt)}
        </p>
      </header>
      <div
        className={letterProseClassName}
        // The body is markdown rendered to HTML at build time by the
        // company:letters-markdown Vite plugin; Tailwind child selectors
        // keep the correspondence typography local to this route.
        dangerouslySetInnerHTML={{ __html: letter.bodyHtml }}
      />
      <Signature />
    </article>
  );
}

function Signature() {
  return (
    <div className="mt-20 md:mt-24">
      <p
        className="font-display italic text-[var(--treatment-ink)] [font-variation-settings:'opsz'_24,'SOFT'_30]"
        style={{ fontSize: "18px", lineHeight: 1.4, margin: 0 }}
      >
        — Shovon
      </p>
      <p
        className="mt-1 font-display italic text-[var(--treatment-muted-meta)] [font-variation-settings:'opsz'_18,'SOFT'_30]"
        style={{ fontSize: "14px", lineHeight: 1.4, margin: 0 }}
      >
        Guardian · Seattle
      </p>
    </div>
  );
}
