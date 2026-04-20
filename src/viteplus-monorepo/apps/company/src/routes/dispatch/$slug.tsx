import { createFileRoute, notFound } from "@tanstack/react-router";
import { useEffect } from "react";
import { postBySlug } from "~/content/dispatch";
import { BodyParagraph, PageShell } from "~/components/page-shell";
import { emitSpan } from "~/lib/telemetry/browser";

export const Route = createFileRoute("/dispatch/$slug")({
  component: DispatchPost,
  loader: ({ params }) => {
    const post = postBySlug(params.slug);
    if (!post) {
      throw notFound();
    }
    return { post };
  },
  head: ({ loaderData }) => ({
    meta: [
      { title: `${loaderData?.post.title ?? "Dispatch"} — Guardian Intelligence` },
      { name: "description", content: loaderData?.post.summary ?? "" },
    ],
  }),
});

function DispatchPost() {
  const { post } = Route.useLoaderData();

  useEffect(() => {
    emitSpan("company.dispatch.post_view", {
      "post.slug": post.slug,
      "post.published_at": post.publishedAt,
    });
  }, [post.slug, post.publishedAt]);

  return (
    <PageShell kicker={`${post.publishedAt} · ${post.kicker}`} heading={post.title}>
      {post.body.map((paragraph, idx) => (
        <BodyParagraph key={idx}>{paragraph}</BodyParagraph>
      ))}
      <p
        className="mt-8 font-mono text-[10px] uppercase tracking-[0.18em]"
        style={{ color: "rgba(245,245,245,0.4)" }}
      >
        Signed · {post.author} · Seattle, Washington
      </p>
    </PageShell>
  );
}
