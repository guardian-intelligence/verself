import { createFileRoute, Link } from "@tanstack/react-router";
import { DISPATCH_META, sortedPosts } from "~/content/dispatch";
import { PageShell } from "~/components/page-shell";

export const Route = createFileRoute("/dispatch/")({
  component: DispatchIndex,
  head: () => ({
    meta: [
      { title: DISPATCH_META.title },
      { name: "description", content: DISPATCH_META.description },
      { property: "og:image", content: "/og/dispatch" },
      { property: "og:image:type", content: "image/svg+xml" },
      { property: "og:image:width", content: "1200" },
      { property: "og:image:height", content: "630" },
      { name: "twitter:card", content: "summary_large_image" },
      { name: "twitter:image", content: "/og/dispatch" },
    ],
    links: [
      {
        rel: "alternate",
        type: "application/rss+xml",
        href: "/dispatch/rss",
        title: DISPATCH_META.title,
      },
    ],
  }),
});

function DispatchIndex() {
  const posts = sortedPosts();
  return (
    <PageShell kicker="The Dispatch" heading="Long-form from Guardian Intelligence.">
      <p
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "16px",
          lineHeight: 1.55,
          color: "rgba(245,245,245,0.72)",
          margin: 0,
        }}
      >
        {DISPATCH_META.description}
      </p>
      <ul className="mt-4 flex flex-col gap-6">
        {posts.map((post) => (
          <li key={post.slug}>
            <Link
              to="/dispatch/$slug"
              params={{ slug: post.slug }}
              className="group flex flex-col gap-1.5 rounded-md px-1 py-2 transition-colors"
              style={{ color: "var(--color-type-iron)" }}
            >
              <span
                className="font-mono text-[10px] uppercase tracking-[0.18em]"
                style={{ color: "rgba(245,245,245,0.45)" }}
              >
                {post.publishedAt} · {post.kicker}
              </span>
              <span
                style={{
                  fontFamily: "'Fraunces', Georgia, serif",
                  fontVariationSettings: '"opsz" 72, "SOFT" 30',
                  fontWeight: 400,
                  fontSize: "28px",
                  lineHeight: 1.15,
                  letterSpacing: "-0.018em",
                }}
              >
                {post.title}
              </span>
              <span
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "15px",
                  lineHeight: 1.55,
                  color: "rgba(245,245,245,0.68)",
                }}
              >
                {post.summary}
              </span>
            </Link>
          </li>
        ))}
      </ul>
      <p
        className="mt-6 font-mono text-[10px] uppercase tracking-[0.18em]"
        style={{ color: "rgba(245,245,245,0.45)" }}
      >
        <a href="/dispatch/rss" style={{ color: "var(--color-flare)" }}>
          RSS →
        </a>
      </p>
    </PageShell>
  );
}
