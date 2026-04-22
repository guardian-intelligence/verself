import { createFileRoute, Link } from "@tanstack/react-router";
import { LETTERS_META, sortedLetters } from "~/content/letters";
import { PageShell } from "~/components/page-shell";
import { ogMeta } from "~/lib/head";

// Letters is the editorial surface. Per brand/voice.md audience split:
// "Iron for customers. Flare for the world. Paper for editorial." — Letters
// must ship on Paper. The PageShell ground prop flips the shell tokens to
// ink-on-paper with a bordeaux accent.

export const Route = createFileRoute("/letters/")({
  component: LettersIndex,
  head: () => ({
    meta: ogMeta({
      slug: "letters",
      title: LETTERS_META.title,
      description: LETTERS_META.description,
    }),
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
    <PageShell ground="paper" kicker="Letters" heading="Long-form from Guardian.">
      <p
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "16px",
          lineHeight: 1.55,
          color: "var(--shell-muted-strong)",
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
              style={{ color: "var(--shell-fg)" }}
            >
              <span
                className="font-mono text-[10px] uppercase tracking-[0.18em]"
                style={{ color: "var(--shell-muted-meta)" }}
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
                  color: "var(--shell-muted)",
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
        style={{ color: "var(--shell-muted-meta)" }}
      >
        <a href="/letters/rss" style={{ color: "var(--shell-accent)" }}>
          RSS →
        </a>
      </p>
    </PageShell>
  );
}
