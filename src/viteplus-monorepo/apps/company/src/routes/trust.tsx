import { createFileRoute } from "@tanstack/react-router";
import { TRUST_META, trust } from "~/content/trust";
import { BodyParagraph, PageShell } from "~/components/page-shell";

export const Route = createFileRoute("/trust")({
  component: TrustPage,
  head: () => ({
    meta: [
      { title: TRUST_META.title },
      { name: "description", content: TRUST_META.description },
    ],
  }),
});

function TrustPage() {
  return (
    <PageShell kicker={trust.kicker} heading={trust.hero}>
      <BodyParagraph>{trust.intro}</BodyParagraph>

      <ul className="mt-6 flex flex-col gap-4">
        {trust.commitments.map((commitment) => (
          <li
            key={commitment.title}
            className="flex flex-col gap-2 rounded-lg p-5"
            style={{
              border: "1px solid rgba(245,245,245,0.12)",
              background: "rgba(245,245,245,0.02)",
            }}
          >
            <span style={{ fontWeight: 600, fontSize: "16px" }}>{commitment.title}</span>
            <p
              style={{
                fontFamily: "'Geist', sans-serif",
                fontSize: "14px",
                lineHeight: 1.55,
                color: "rgba(245,245,245,0.75)",
                margin: 0,
              }}
            >
              {commitment.body}
            </p>
            <a
              href={commitment.canonical.href}
              className="font-mono text-[11px] uppercase tracking-[0.16em]"
              style={{ color: "var(--color-flare)" }}
            >
              {commitment.canonical.label} →
            </a>
          </li>
        ))}
      </ul>

      <p
        className="mt-8"
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "13px",
          lineHeight: 1.55,
          color: "rgba(245,245,245,0.58)",
          margin: 0,
        }}
      >
        {trust.footerNote}
      </p>
    </PageShell>
  );
}
