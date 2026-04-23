import { createFileRoute } from "@tanstack/react-router";
import { CAREERS_META, careers } from "~/content/careers";
import { BodyParagraph, PageShell } from "~/components/page-shell";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/careers")({
  component: CareersPage,
  head: () => ({
    meta: ogMeta({
      slug: "careers",
      title: CAREERS_META.title,
      description: CAREERS_META.description,
    }),
  }),
});

function CareersPage() {
  return (
    <PageShell kicker={careers.kicker} heading={careers.hero}>
      {careers.paragraphs.map((paragraph, idx) => (
        <BodyParagraph key={idx}>{paragraph}</BodyParagraph>
      ))}

      {careers.openings.length === 0 ? (
        <p
          className="mt-8 rounded-md p-5"
          style={{
            border: "1px dashed var(--treatment-surface-border)",
            color: "var(--treatment-muted)",
            fontFamily: "'Geist', sans-serif",
            fontSize: "15px",
            lineHeight: 1.55,
          }}
        >
          {careers.emptyState}{" "}
          <a href={`mailto:${careers.contactEmail}`} style={{ color: "var(--treatment-accent)" }}>
            {careers.contactEmail}
          </a>
        </p>
      ) : (
        <ul className="mt-8 flex flex-col gap-3">
          {careers.openings.map((opening) => (
            <li
              key={opening.title}
              className="flex flex-col gap-1 rounded-md p-5"
              style={{
                border: "1px solid var(--treatment-surface-border)",
                background: "var(--treatment-surface-subtle)",
              }}
            >
              <span style={{ fontWeight: 600 }}>{opening.title}</span>
              <span style={{ color: "var(--treatment-muted)" }}>{opening.description}</span>
            </li>
          ))}
        </ul>
      )}
    </PageShell>
  );
}
