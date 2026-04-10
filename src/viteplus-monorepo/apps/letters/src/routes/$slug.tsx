import { createFileRoute, ClientOnly } from "@tanstack/react-router";
import { useMemo } from "react";
import { useLiveQuery } from "@tanstack/react-db";
import { createPublishedPostsCollection, type ElectricPost } from "~/lib/collections";
import { TiptapRenderer } from "~/components/tiptap-renderer";
import { ClapButton } from "~/components/clap-button";
import { ReadingProgress } from "~/components/reading-progress";
import { getPostBySlug } from "~/server-fns/posts";
import { formatPostDate, parsePostContent } from "~/lib/post-utils";

export const Route = createFileRoute("/$slug")({
  component: PostPage,
  loader: async ({ params }) => {
    const post = await getPostBySlug({ data: { slug: params.slug } });
    return { post };
  },
  head: ({ loaderData }) => {
    const post = loaderData?.post;
    if (!post) return { meta: [{ title: "Not Found" }] };
    return {
      meta: [
        { title: post.title },
        { name: "description", content: post.subtitle || post.title },
        { property: "og:title", content: post.title },
        { property: "og:description", content: post.subtitle || post.title },
        ...(post.cover_image_url ? [{ property: "og:image", content: post.cover_image_url }] : []),
        { property: "og:type", content: "article" },
        { property: "article:published_time", content: post.published_at || "" },
      ],
    };
  },
});

function PostPage() {
  const { post: ssrPost } = Route.useLoaderData();
  if (!ssrPost) {
    return (
      <div className="max-w-3xl mx-auto px-6 py-24 text-center">
        <h1 className="text-2xl font-bold mb-2">Post not found</h1>
        <p className="text-muted-foreground">This post may have been removed or unpublished.</p>
      </div>
    );
  }

  return (
    <ClientOnly fallback={<PostArticle post={ssrPost} />}>
      <LivePostArticle ssrPost={ssrPost} />
    </ClientOnly>
  );
}

function LivePostArticle({
  ssrPost,
}: {
  ssrPost: NonNullable<Awaited<ReturnType<typeof getPostBySlug>>>;
}) {
  const collection = useMemo(() => createPublishedPostsCollection(), []);
  const { data: livePosts } = useLiveQuery((q) => q.from({ p: collection }), [collection]);
  const livePost = useMemo(
    () => (livePosts as ElectricPost[] | undefined)?.find((post) => post.slug === ssrPost.slug),
    [livePosts, ssrPost.slug],
  );
  return <PostArticle post={livePost ?? ssrPost} />;
}

function PostArticle({
  post,
}: {
  post: NonNullable<Awaited<ReturnType<typeof getPostBySlug>>> | ElectricPost;
}) {
  const totalClaps =
    typeof post.total_claps === "number" ? post.total_claps : Number(post.total_claps) || 0;

  return (
    <>
      <ClientOnly fallback={null}>
        <ReadingProgress />
      </ClientOnly>

      {/* Cover image */}
      {post.cover_image_url && (
        <div className="w-full max-h-[480px] overflow-hidden">
          <img src={post.cover_image_url} alt="" className="w-full h-full object-cover" />
        </div>
      )}

      <article className="max-w-3xl mx-auto px-6 py-12">
        {/* Title block */}
        <header className="mb-10">
          <h1
            className="text-4xl font-black tracking-tight leading-tight mb-3"
            style={{ fontFamily: "'Playfair Display', serif" }}
          >
            {post.title}
          </h1>
          {post.subtitle && <p className="text-xl text-muted-foreground mb-4">{post.subtitle}</p>}
          <div className="flex items-center gap-3 text-sm text-muted-foreground">
            {post.author_name && <span>{post.author_name}</span>}
            {post.author_name && <span aria-hidden>·</span>}
            <span>{formatPostDate(post.published_at, "long")}</span>
            <span aria-hidden>·</span>
            <span>{post.reading_time_minutes} min read</span>
          </div>
        </header>

        {/* Content */}
        <TiptapRenderer content={parsePostContent(post.content)} className="prose-letters" />
      </article>

      {/* Clap button */}
      <ClientOnly fallback={null}>
        <ClapButton slug={post.slug} totalClaps={totalClaps} />
      </ClientOnly>
    </>
  );
}
