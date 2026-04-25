import { createFileRoute } from "@tanstack/react-router";
import { trace } from "@opentelemetry/api";
import { ogSpecFor } from "~/og/catalog";
import { buildOGCard, formatOGError } from "~/og/template";

// Dynamic OG card endpoint. Paths: /og/home, /og/design, etc. Served as
// image/svg+xml because the company app is node-only (no native SVG→PNG
// dep yet); social platforms that demand PNG will fall back to a pre-rendered
// copy under public/og/*.png in a future iteration.
//
// Emits company.og.render on every request so the operator can see which
// cards actually unfurl in the wild. Voice failures are 500s with an
// explicit payload and og.voice_pass=false — loud failures, per the coding
// contract.

const TRACER = trace.getTracer("verself/company-og", "0.1.0");

// Some social crawlers (and Slack's debugger) issue HEAD before GET to sniff
// content-type before unfurling. Return the same headers as GET would, with
// an empty body — otherwise the framework default is text/html and the card
// is silently refused by strict validators.
function headHandler({ params }: { params: { slug: string } }): Response {
  const slug = params.slug.replace(/\.svg$|\.png$/, "");
  const spec = ogSpecFor(slug);
  if (!spec) {
    return new Response(null, { status: 404, headers: { "content-type": "text/plain" } });
  }
  return new Response(null, {
    status: 200,
    headers: {
      "content-type": "image/svg+xml; charset=utf-8",
      "cache-control": "public, max-age=600, s-maxage=600",
    },
  });
}

export const Route = createFileRoute("/og/$slug")({
  server: {
    handlers: {
      HEAD: headHandler,
      GET: ({ params }) => {
        const slug = params.slug.replace(/\.svg$|\.png$/, "");
        const spec = ogSpecFor(slug);
        return TRACER.startActiveSpan("company.og.render", (span) => {
          span.setAttribute("og.slug", slug);
          try {
            if (!spec) {
              span.setAttribute("og.voice_pass", "false");
              span.setAttribute("og.error", "slug_not_found");
              span.setStatus({ code: 2, message: "slug not found" });
              return new Response(`og slug not found: ${slug}`, {
                status: 404,
                headers: { "content-type": "text/plain" },
              });
            }
            const result = buildOGCard(spec);
            if (!result.ok) {
              span.setAttribute("og.voice_pass", "false");
              span.setAttribute("og.error_kind", result.error.kind);
              span.setStatus({ code: 2, message: formatOGError(result.error) });
              return new Response(`og build failed: ${formatOGError(result.error)}`, {
                status: 500,
                headers: { "content-type": "text/plain" },
              });
            }
            span.setAttribute("og.voice_pass", "true");
            span.setAttribute("og.content_hash", result.contentHash);
            span.setAttribute("og.bytes", String(result.svg.length));
            return new Response(result.svg, {
              status: 200,
              headers: {
                "content-type": "image/svg+xml; charset=utf-8",
                "cache-control": "public, max-age=600, s-maxage=600",
              },
            });
          } finally {
            span.end();
          }
        });
      },
    },
  },
});
