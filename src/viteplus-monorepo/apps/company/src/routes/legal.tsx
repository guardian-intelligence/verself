import { createFileRoute } from "@tanstack/react-router";
import { LEGAL_META, legal } from "~/content/legal";
import { BodyParagraph, PageShell } from "~/components/page-shell";

export const Route = createFileRoute("/legal")({
  component: LegalPage,
  head: () => ({
    meta: [
      { title: LEGAL_META.title },
      { name: "description", content: LEGAL_META.description },
    ],
  }),
});

function LegalPage() {
  return (
    <PageShell kicker={legal.kicker} heading={legal.hero}>
      <BodyParagraph>{legal.intro}</BodyParagraph>

      <ul className="mt-6 flex flex-col gap-3">
        {legal.mirrors.map((mirror) => (
          <li key={mirror.title}>
            <a
              href={mirror.href}
              className="group flex flex-col gap-1 rounded-md p-4 transition-colors"
              style={{
                border: "1px solid rgba(245,245,245,0.12)",
                background: "rgba(245,245,245,0.02)",
                color: "var(--color-type-iron)",
              }}
            >
              <span style={{ fontWeight: 600, fontSize: "15px" }}>{mirror.title}</span>
              <span
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "13px",
                  lineHeight: 1.55,
                  color: "rgba(245,245,245,0.68)",
                }}
              >
                {mirror.body}
              </span>
              <span
                className="font-mono text-[10px] uppercase tracking-[0.16em]"
                style={{ color: "rgba(245,245,245,0.45)" }}
              >
                {mirror.href.replace("https://", "")} →
              </span>
            </a>
          </li>
        ))}
      </ul>
    </PageShell>
  );
}
