import { createFileRoute } from "@tanstack/react-router";
import { COMPANY_META, company } from "~/content/company";
import { BodyParagraph, PageShell } from "~/components/page-shell";

export const Route = createFileRoute("/company")({
  component: CompanyPage,
  head: () => ({
    meta: [
      { title: COMPANY_META.title },
      { name: "description", content: COMPANY_META.description },
    ],
  }),
});

function CompanyPage() {
  return (
    <PageShell kicker={company.kicker} heading={company.hero}>
      {company.paragraphs.map((paragraph, idx) => (
        <BodyParagraph key={idx}>{paragraph}</BodyParagraph>
      ))}

      <div className="mt-10 grid gap-4 md:grid-cols-3">
        {company.values.map((value) => (
          <div
            key={value.name}
            className="flex flex-col gap-2 rounded-lg p-5"
            style={{
              border: "1px solid rgba(245,245,245,0.12)",
              background: "rgba(245,245,245,0.02)",
            }}
          >
            <span
              className="font-mono text-[10px] uppercase tracking-[0.18em]"
              style={{ color: "rgba(245,245,245,0.45)" }}
            >
              {value.name}
            </span>
            <p
              style={{
                fontFamily: "'Geist', sans-serif",
                fontSize: "14px",
                lineHeight: 1.55,
                color: "rgba(245,245,245,0.75)",
                margin: 0,
              }}
            >
              {value.body}
            </p>
          </div>
        ))}
      </div>
    </PageShell>
  );
}
