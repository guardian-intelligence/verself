import { createFileRoute } from "@tanstack/react-router";
import { CHANGELOG_META, changelog } from "~/content/changelog";
import { PageShell } from "~/components/page-shell";

export const Route = createFileRoute("/changelog")({
  component: ChangelogPage,
  head: () => ({
    meta: [
      { title: CHANGELOG_META.title },
      { name: "description", content: CHANGELOG_META.description },
    ],
  }),
});

function ChangelogPage() {
  return (
    <PageShell kicker="What shipped, when" heading="anveio.com changelog.">
      <ul className="flex flex-col gap-6">
        {changelog.map((entry) => (
          <li key={entry.date} className="flex flex-col gap-2">
            <span
              className="font-mono text-[10px] uppercase tracking-[0.18em]"
              style={{ color: "rgba(245,245,245,0.45)" }}
            >
              {entry.date}
            </span>
            <span
              style={{
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 72, "SOFT" 30',
                fontWeight: 400,
                fontSize: "22px",
                lineHeight: 1.2,
                letterSpacing: "-0.015em",
              }}
            >
              {entry.title}
            </span>
            <p
              style={{
                fontFamily: "'Geist', sans-serif",
                fontSize: "15px",
                lineHeight: 1.55,
                color: "rgba(245,245,245,0.75)",
                margin: 0,
              }}
            >
              {entry.body}
            </p>
          </li>
        ))}
      </ul>
    </PageShell>
  );
}
