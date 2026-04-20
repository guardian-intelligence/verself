import { createFileRoute } from "@tanstack/react-router";
import { SOLUTIONS, SOLUTIONS_META } from "~/content/solutions";
import { PageShell } from "~/components/page-shell";

export const Route = createFileRoute("/solutions")({
  component: SolutionsPage,
  head: () => ({
    meta: [
      { title: SOLUTIONS_META.title },
      { name: "description", content: SOLUTIONS_META.description },
      { property: "og:image", content: "/og/solutions" },
      { property: "og:image:type", content: "image/svg+xml" },
      { property: "og:image:width", content: "1200" },
      { property: "og:image:height", content: "630" },
      { name: "twitter:card", content: "summary_large_image" },
      { name: "twitter:image", content: "/og/solutions" },
    ],
  }),
});

function SolutionsPage() {
  return (
    <PageShell kicker="One house, one platform." heading="Metal Platform.">
      <p
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "16px",
          lineHeight: 1.55,
          color: "rgba(245,245,245,0.72)",
          margin: 0,
        }}
      >
        {SOLUTIONS_META.description}
      </p>
      <ul className="mt-6 flex flex-col gap-4">
        {SOLUTIONS.map((solution) => (
          <li key={solution.slug}>
            <a
              href={solution.href}
              className="group flex flex-col gap-3 rounded-lg border p-5 transition-colors"
              style={{
                borderColor: "rgba(245,245,245,0.12)",
                background: "rgba(245,245,245,0.02)",
                color: "var(--color-type-iron)",
              }}
            >
              <span
                className="font-mono text-[10px] uppercase tracking-[0.18em]"
                style={{ color: "rgba(245,245,245,0.45)" }}
              >
                {solution.kicker}
              </span>
              <span
                style={{
                  fontFamily: "'Fraunces', Georgia, serif",
                  fontVariationSettings: '"opsz" 72, "SOFT" 30',
                  fontWeight: 400,
                  fontSize: "32px",
                  lineHeight: 1.1,
                  letterSpacing: "-0.02em",
                }}
              >
                {solution.name}
              </span>
              <span
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "16px",
                  lineHeight: 1.5,
                  color: "rgba(245,245,245,0.82)",
                }}
              >
                {solution.oneLiner}
              </span>
              <span
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "14px",
                  lineHeight: 1.55,
                  color: "rgba(245,245,245,0.6)",
                }}
              >
                {solution.description}
              </span>
              <ul
                className="mt-2 flex flex-col gap-2 border-t pt-3"
                style={{ borderColor: "rgba(245,245,245,0.08)" }}
              >
                {solution.products.map((product) => (
                  <li
                    key={product.name}
                    className="flex flex-col gap-0.5 md:flex-row md:items-baseline md:gap-4"
                  >
                    <span
                      className="font-mono text-[10px] uppercase tracking-[0.16em] md:w-24 md:shrink-0"
                      style={{ color: "rgba(245,245,245,0.4)" }}
                    >
                      {labelFor(product.kind)}
                    </span>
                    <span className="flex flex-col gap-0.5">
                      <span
                        style={{
                          fontFamily: "'Geist', sans-serif",
                          fontWeight: 500,
                          fontSize: "14px",
                          color: "var(--color-type-iron)",
                        }}
                      >
                        {product.name}
                      </span>
                      <span
                        style={{
                          fontFamily: "'Geist', sans-serif",
                          fontSize: "13px",
                          lineHeight: 1.55,
                          color: "rgba(245,245,245,0.6)",
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
