import { createFileRoute } from "@tanstack/react-router";
import { CAREERS_META, careers } from "~/content/careers";
import { BodyParagraph, PageShell } from "~/components/page-shell";

export const Route = createFileRoute("/careers")({
  component: CareersPage,
  head: () => ({
    meta: [
      { title: CAREERS_META.title },
      { name: "description", content: CAREERS_META.description },
    ],
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
            border: "1px dashed rgba(245,245,245,0.15)",
            color: "rgba(245,245,245,0.72)",
            fontFamily: "'Geist', sans-serif",
            fontSize: "15px",
            lineHeight: 1.55,
          }}
        >
          {careers.emptyState}{" "}
          <a href={`mailto:${careers.contactEmail}`} style={{ color: "var(--color-flare)" }}>
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
                border: "1px solid rgba(245,245,245,0.12)",
                background: "rgba(245,245,245,0.02)",
              }}
            >
              <span style={{ fontWeight: 600 }}>{opening.title}</span>
              <span style={{ color: "rgba(245,245,245,0.7)" }}>{opening.description}</span>
            </li>
          ))}
        </ul>
      )}
    </PageShell>
  );
}
