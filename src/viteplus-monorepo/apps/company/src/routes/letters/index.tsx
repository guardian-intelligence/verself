import { createFileRoute, Link } from "@tanstack/react-router";
import { LETTERS_META, sortedLetters } from "~/content/letters";
import { PageShell } from "~/components/page-shell";

export const Route = createFileRoute("/letters/")({
  component: LettersIndex,
  head: () => ({
    meta: [
      { title: LETTERS_META.title },
      { name: "description", content: LETTERS_META.description },
      { property: "og:image", content: "/og/letters" },
      { property: "og:image:type", content: "image/svg+xml" },
      { property: "og:image:width", content: "1200" },
      { property: "og:image:height", content: "630" },
      { name: "twitter:card", content: "summary_large_image" },
      { name: "twitter:image", content: "/og/letters" },
    ],
    links: [
      {
        rel: "alternate",
        type: "application/rss+xml",
        href: "/letters/rss",
        title: LETTERS_META.title,
      },
    ],
  }),
});

function LettersIndex() {
  const letters = sortedLetters();
  return (
    <PageShell kicker="Letters" heading="Long-form from Guardian Intelligence.">
      <p
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "16px",
          lineHeight: 1.55,
          color: "rgba(245,245,245,0.72)",
          margin: 0,
        }}
      >
        {LETTERS_META.description}
      </p>
      <ul className="mt-4 flex flex-col gap-6">
        {letters.map((letter) => (
          <li key={letter.slug}>
            <Link
              to="/letters/$slug"
              params={{ slug: letter.slug }}
              className="group flex flex-col gap-1.5 rounded-md px-1 py-2 transition-colors"
              style={{ color: "var(--color-type-iron)" }}
            >
              <span
                className="font-mono text-[10px] uppercase tracking-[0.18em]"
                style={{ color: "rgba(245,245,245,0.45)" }}
              >
                {letter.publishedAt} · {letter.kicker}
              </span>
              <span
                style={{
                  fontFamily: "'Fraunces', Georgia, serif",
                  fontVariationSettings: '"opsz" 72, "SOFT" 30',
                  fontWeight: 400,
                  fontSize: "28px",
                  lineHeight: 1.15,
                  letterSpacing: "-0.018em",
                }}
              >
                {letter.title}
              </span>
              <span
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "15px",
                  lineHeight: 1.55,
                  color: "rgba(245,245,245,0.68)",
                }}
              >
                {letter.summary}
              </span>
            </Link>
          </li>
        ))}
      </ul>
      <p
        className="mt-6 font-mono text-[10px] uppercase tracking-[0.18em]"
        style={{ color: "rgba(245,245,245,0.45)" }}
      >
        <a href="/letters/rss" style={{ color: "var(--color-flare)" }}>
          RSS →
        </a>
      </p>
    </PageShell>
  );
}
