import { createFileRoute } from "@tanstack/react-router";
import { SOLUTIONS, SOLUTIONS_META } from "~/content/solutions";
import { PageShell } from "~/components/page-shell";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/_workshop/solutions")({
  component: SolutionsPage,
  head: () => ({
    meta: ogMeta({
      slug: "solutions",
      title: SOLUTIONS_META.title,
      description: SOLUTIONS_META.description,
    }),
  }),
});

function SolutionsPage() {
  return (
    <PageShell kicker="One house, one platform." heading="Verself Platform.">
      <p
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "16px",
          lineHeight: 1.55,
          color: "var(--treatment-muted)",
          margin: 0,
        }}
      >
        {SOLUTIONS_META.description}
      </p>
      <ul className="mt-6 flex flex-col gap-12">
        {SOLUTIONS.map((solution) => (
          <li key={solution.slug}>
            <a
              href={solution.href}
              className="group flex flex-col gap-3 transition-colors"
              style={{ color: "var(--treatment-ink)" }}
            >
              <span
                className="font-mono text-[10px] uppercase tracking-[0.18em]"
                style={{ color: "var(--treatment-muted-faint)" }}
              >
                {solution.kicker}
              </span>
              <span
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontWeight: 600,
                  fontSize: "28px",
                  lineHeight: 1.1,
                  letterSpacing: "-0.018em",
                }}
              >
                {solution.name}
              </span>
              <span
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "16px",
                  lineHeight: 1.5,
                  color: "var(--treatment-muted-strong)",
                }}
              >
                {solution.oneLiner}
              </span>
              <span
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "14px",
                  lineHeight: 1.55,
                  color: "var(--treatment-muted-meta)",
                }}
              >
                {solution.description}
              </span>
              <ul
                className="mt-2 flex flex-col gap-2 border-t pt-3"
                style={{ borderColor: "var(--treatment-surface-border)" }}
              >
                {solution.products.map((product) => (
                  <li
                    key={product.name}
                    className="flex flex-col gap-0.5 md:flex-row md:items-baseline md:gap-4"
                  >
                    <span
                      className="font-mono text-[10px] uppercase tracking-[0.16em] md:w-24 md:shrink-0"
                      style={{ color: "var(--treatment-muted-faint)" }}
                    >
                      {labelFor(product.kind)}
                    </span>
                    <span className="flex flex-col gap-0.5">
                      <span
                        style={{
                          fontFamily: "'Geist', sans-serif",
                          fontWeight: 500,
                          fontSize: "14px",
                          color: "var(--treatment-ink)",
                        }}
                      >
                        {product.name}
                      </span>
                      <span
                        style={{
                          fontFamily: "'Geist', sans-serif",
                          fontSize: "13px",
                          lineHeight: 1.55,
                          color: "var(--treatment-muted-meta)",
                        }}
                      >
                        {product.blurb}
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            </a>
          </li>
        ))}
      </ul>
    </PageShell>
  );
}

function labelFor(kind: "service" | "web-app" | "cli" | "sdk"): string {
  switch (kind) {
    case "service":
      return "Services";
    case "web-app":
      return "Web app";
    case "cli":
      return "CLI";
    case "sdk":
      return "SDKs";
  }
}
