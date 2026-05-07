import { createFileRoute, notFound } from "@tanstack/react-router";
import { useEffect } from "react";
import { letterBySlug } from "~/content/letters";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

// Individual letter. Editorial → Paper ground, consistent with /letters/.
// The detail page renders without PageShell so the writing column can hold
// its own margin and read like correspondence instead of a section page.

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
  "mt-10 w-full max-w-full font-display text-[var(--treatment-muted-strong)] md:max-w-[76ch]",
  "[font-variation-settings:'opsz'_18,'SOFT'_0]",
  "[overflow-wrap:break-word]",
  "[&>*+*]:mt-7",
  "[&>p]:text-[18px] [&>p]:font-normal [&>p]:leading-[1.62] md:[&>p]:text-[clamp(19px,1.45vw,21px)]",
  "[&>blockquote]:border-l-2 [&>blockquote]:border-[var(--treatment-rule-color)] [&>blockquote]:pl-5 [&>blockquote]:italic",
  "[&>blockquote]:text-[18px] [&>blockquote]:leading-[1.62] md:[&>blockquote]:text-[clamp(19px,1.45vw,21px)]",
  "[&>ul]:list-disc [&>ol]:list-decimal [&>ul]:pl-7 [&>ol]:pl-7",
  "[&_li]:mt-2 [&_li]:text-[18px] [&_li]:leading-[1.62] md:[&_li]:text-[clamp(19px,1.45vw,21px)]",
  "[&_a]:text-[var(--treatment-ink)] [&_a]:underline [&_a]:decoration-[1px] [&_a]:underline-offset-[0.18em]",
  "[&>h2]:mt-14 [&>h2]:font-display [&>h2]:text-[clamp(28px,3vw,38px)] [&>h2]:font-normal [&>h2]:leading-[1.12]",
  "[&>h3]:mt-12 [&>h3]:font-display [&>h3]:text-[clamp(22px,2.4vw,28px)] [&>h3]:font-normal [&>h3]:leading-[1.18]",
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
    <article className="mx-auto w-full max-w-6xl overflow-x-clip px-5 pb-20 pt-14 sm:px-8 md:px-6 md:pb-28 md:pt-20">
      <div className="ml-0 w-full max-w-full md:ml-[6vw] md:max-w-[82ch] lg:ml-[6.5rem] xl:ml-[7.5rem]">
        <header className="max-w-[76ch]">
          <p className="font-mono text-[12px] leading-none tracking-[0.01em] text-[var(--treatment-muted-meta)] md:text-[13px]">
            {formatDate(letter.publishedAt)}
          </p>
          <h1 className="mt-7 max-w-[20ch] font-display text-[clamp(38px,4.2vw,58px)] font-normal leading-[1.06] tracking-normal text-[var(--treatment-ink)] [font-variation-settings:'opsz'_96,'SOFT'_18]">
            {letter.title}
          </h1>
        </header>
        <div
          className={letterProseClassName}
          // The body is markdown rendered to HTML at build time by the
          // company:letters-markdown Vite plugin; Tailwind child selectors
          // keep the correspondence typography local to this route.
          dangerouslySetInnerHTML={{ __html: letter.bodyHtml }}
        />
      </div>
    </article>
  );
}
