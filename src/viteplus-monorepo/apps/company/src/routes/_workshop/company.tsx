import { createFileRoute } from "@tanstack/react-router";
import { COMPANY_META, company } from "~/content/company";
import { BodyParagraph, PageShell } from "~/components/page-shell";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/_workshop/company")({
  component: CompanyPage,
  head: () => ({
    meta: ogMeta({
      slug: "company",
      title: COMPANY_META.title,
      description: COMPANY_META.description,
    }),
  }),
});

function CompanyPage() {
  return (
    <PageShell kicker={company.kicker} heading={company.hero}>
      {company.paragraphs.map((paragraph, idx) => (
        <BodyParagraph key={idx}>{paragraph}</BodyParagraph>
      ))}

      <div className="mt-10 grid gap-8 md:grid-cols-3">
        {company.values.map((value) => (
          <div key={value.name} className="flex flex-col gap-2">
            <span
              className="font-mono text-[10px] uppercase tracking-[0.18em]"
              style={{ color: "var(--treatment-muted-faint)" }}
            >
              {value.name}
            </span>
            <p
              style={{
                fontFamily: "'Geist', sans-serif",
                fontSize: "14px",
                lineHeight: 1.55,
                color: "var(--treatment-muted)",
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
