import { ClientOnly, createFileRoute, notFound } from "@tanstack/react-router";
import { getPost, type Post } from "~/posts";
import { ReadingProgress } from "~/components/reading-progress";
import { formatPublishedDate, getSiteOrigin } from "~/lib/site";

export const Route = createFileRoute("/$slug")({
  component: PostPage,
  loader: async ({ params }) => {
    const post = getPost(params.slug);
    if (!post) throw notFound();
    return { meta: post.meta, siteOrigin: await getSiteOrigin() };
  },
  notFoundComponent: NotFoundComponent,
  head: ({ loaderData }) => {
    if (!loaderData) return { meta: [{ title: "Not found" }] };
    const { meta, siteOrigin } = loaderData;
    const url = `${siteOrigin}/${meta.slug}`;
    const isoDate = new Date(meta.publishedAt).toISOString();

    const jsonLd = {
      "@context": "https://schema.org",
      "@type": "BlogPosting",
      headline: meta.title,
      description: meta.description,
      datePublished: isoDate,
      dateModified: isoDate,
      author: { "@type": "Person", name: meta.author },
      mainEntityOfPage: { "@type": "WebPage", "@id": url },
      ...(meta.coverImageUrl ? { image: meta.coverImageUrl } : {}),
    };

    return {
      meta: [
        { title: meta.title },
        { name: "description", content: meta.description },
        { name: "author", content: meta.author },
        { property: "og:type", content: "article" },
        { property: "og:title", content: meta.title },
        { property: "og:description", content: meta.description },
        { property: "og:url", content: url },
        { property: "article:published_time", content: isoDate },
        { property: "article:author", content: meta.author },
        ...(meta.coverImageUrl ? [{ property: "og:image", content: meta.coverImageUrl }] : []),
        { name: "twitter:card", content: meta.coverImageUrl ? "summary_large_image" : "summary" },
        { name: "twitter:title", content: meta.title },
        { name: "twitter:description", content: meta.description },
      ],
      links: [{ rel: "canonical", href: url }],
      scripts: [
        {
          type: "application/ld+json",
          children: JSON.stringify(jsonLd),
        },
      ],
    };
  },
});

type LoaderData = { meta: Post["meta"]; siteOrigin: string };

function PostPage() {
  const { meta } = Route.useLoaderData() as LoaderData;
  const post = getPost(meta.slug);
  if (!post) return <NotFoundComponent />;
  const Body = post.Body;

  return (
    <>
      <ClientOnly fallback={null}>
        <ReadingProgress />
      </ClientOnly>

      {meta.coverImageUrl && (
        <div className="w-full max-h-[480px] overflow-hidden">
          <img src={meta.coverImageUrl} alt="" className="w-full h-full object-cover" />
        </div>
      )}

      <article className="max-w-3xl mx-auto px-6 py-12">
        <header className="mb-10">
          <h1
            className="text-4xl font-black tracking-tight leading-tight mb-3"
            style={{ fontFamily: "'Playfair Display', serif" }}
          >
            {meta.title}
          </h1>
          {meta.subtitle && <p className="text-xl text-muted-foreground mb-4">{meta.subtitle}</p>}
          <div className="flex items-center gap-3 text-sm text-muted-foreground">
            <span>{meta.author}</span>
            <span aria-hidden>·</span>
            <time dateTime={meta.publishedAt}>{formatPublishedDate(meta.publishedAt)}</time>
            <span aria-hidden>·</span>
            <span>{meta.readingMinutes} min read</span>
          </div>
        </header>

        <div className="prose-letters">
          <Body />
        </div>
      </article>
    </>
  );
}

function NotFoundComponent() {
  return (
    <div className="max-w-3xl mx-auto px-6 py-24 text-center">
      <h1 className="text-2xl font-bold mb-2">Post not found</h1>
      <p className="text-muted-foreground">This post may have been removed or never existed.</p>
    </div>
  );
}
