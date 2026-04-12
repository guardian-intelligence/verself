import { createFileRoute, Link } from "@tanstack/react-router";
import { listPosts, type Post } from "~/posts";
import { formatPublishedDate, getSiteOrigin } from "~/lib/site";

export const Route = createFileRoute("/")({
  component: HomePage,
  loader: async () => ({
    posts: listPosts().map((post) => post.meta),
    siteOrigin: await getSiteOrigin(),
  }),
  head: ({ loaderData }) => {
    const origin = loaderData?.siteOrigin ?? "";
    const title = "Letters";
    const description = "Long-form notes from the Forge Metal team.";
    return {
      meta: [
        { title },
        { name: "description", content: description },
        { property: "og:type", content: "website" },
        { property: "og:title", content: title },
        { property: "og:description", content: description },
        ...(origin ? [{ property: "og:url", content: origin }] : []),
        { name: "twitter:card", content: "summary" },
        { name: "twitter:title", content: title },
        { name: "twitter:description", content: description },
      ],
      links: origin ? [{ rel: "canonical", href: origin }] : [],
    };
  },
});

type IndexLoaderData = { posts: ReadonlyArray<Post["meta"]>; siteOrigin: string };

function HomePage() {
  const { posts } = Route.useLoaderData() as IndexLoaderData;

  return (
    <div className="max-w-3xl mx-auto px-6 py-12">
      <header className="mb-12">
        <h1
          className="text-4xl font-black tracking-tight mb-2"
          style={{ fontFamily: "'Playfair Display', serif" }}
        >
          Letters
        </h1>
        <p className="text-lg text-muted-foreground">Thoughts, ideas, and explorations.</p>
      </header>

      {posts.length === 0 ? (
        <p className="text-muted-foreground py-12 text-center">No posts yet.</p>
      ) : (
        <ul className="divide-y divide-border">
          {posts.map((meta) => (
            <li key={meta.slug} className="py-6">
              <PostCard meta={meta} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function PostCard({ meta }: { meta: Post["meta"] }) {
  return (
    <article>
      <Link to="/$slug" params={{ slug: meta.slug }} className="group block">
        <h2
          className="text-2xl font-bold tracking-tight group-hover:underline"
          style={{ fontFamily: "'Playfair Display', serif" }}
        >
          {meta.title}
        </h2>
        {meta.subtitle && <p className="mt-1 text-base text-muted-foreground">{meta.subtitle}</p>}
        <div className="mt-2 flex items-center gap-3 text-sm text-muted-foreground">
          <span>{meta.author}</span>
          <span aria-hidden>·</span>
          <time dateTime={meta.publishedAt}>{formatPublishedDate(meta.publishedAt)}</time>
          <span aria-hidden>·</span>
          <span>{meta.readingMinutes} min read</span>
        </div>
      </Link>
    </article>
  );
}
