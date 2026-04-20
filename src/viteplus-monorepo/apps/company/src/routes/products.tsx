import { createFileRoute } from "@tanstack/react-router";
import { PRODUCTS, PRODUCTS_META } from "~/content/products";
import { PageShell } from "~/components/page-shell";

export const Route = createFileRoute("/products")({
  component: ProductsPage,
  head: () => ({
    meta: [
      { title: PRODUCTS_META.title },
      { name: "description", content: PRODUCTS_META.description },
      { property: "og:image", content: "/og/products" },
      { property: "og:image:type", content: "image/svg+xml" },
      { property: "og:image:width", content: "1200" },
      { property: "og:image:height", content: "630" },
      { name: "twitter:card", content: "summary_large_image" },
      { name: "twitter:image", content: "/og/products" },
    ],
  }),
});

function ProductsPage() {
  return (
    <PageShell kicker="One house. Three products." heading="Metal. Console. Letters.">
      <p
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "16px",
          lineHeight: 1.55,
          color: "rgba(245,245,245,0.72)",
          margin: 0,
        }}
      >
        {PRODUCTS_META.description}
      </p>
      <ul className="mt-6 flex flex-col gap-4">
        {PRODUCTS.map((product) => (
          <li key={product.slug}>
            <a
              href={product.href}
              className="group flex flex-col gap-2 rounded-lg border p-5 transition-colors"
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
                {product.kicker}
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
                {product.name}
              </span>
              <span
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "16px",
                  lineHeight: 1.5,
                  color: "rgba(245,245,245,0.82)",
                }}
              >
                {product.oneLiner}
              </span>
              <span
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "14px",
                  lineHeight: 1.55,
                  color: "rgba(245,245,245,0.6)",
                }}
              >
                {product.description}
              </span>
            </a>
          </li>
        ))}
      </ul>
    </PageShell>
  );
}
