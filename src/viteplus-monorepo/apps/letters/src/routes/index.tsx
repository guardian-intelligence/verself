import { ClientOnly, createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { useLiveQuery } from "@tanstack/react-db";
import { createPublishedPostsCollection, type ElectricPost } from "~/lib/collections";
import { PostCard } from "~/components/post-card";
import { sortPostsByPublishedAt } from "~/lib/post-utils";
import { listPublishedPosts } from "~/server-fns/posts";

export const Route = createFileRoute("/")({
  loader: async () => ({
    posts: await listPublishedPosts(),
  }),
  component: HomePage,
  head: () => ({
    meta: [{ title: "Letters" }, { name: "description", content: "Thoughts and ideas" }],
  }),
});

function HomePage() {
  const { posts } = Route.useLoaderData() as { posts: ElectricPost[] };
  const fallbackPosts = sortPostsByPublishedAt(posts);

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

      <ClientOnly fallback={<PostsList posts={fallbackPosts} />}>
        <LivePostsList initialPosts={fallbackPosts} />
      </ClientOnly>
    </div>
  );
}

function LivePostsList({ initialPosts }: { initialPosts: ElectricPost[] }) {
  const collection = useMemo(() => createPublishedPostsCollection(), []);
  const { data: posts } = useLiveQuery((q) => q.from({ p: collection }), [collection]);
  const sortedPosts = useMemo(
    () => sortPostsByPublishedAt((posts as ElectricPost[] | undefined) ?? initialPosts),
    [initialPosts, posts],
  );
  return <PostsList posts={sortedPosts} />;
}

function PostsList({ posts }: { posts: ReadonlyArray<ElectricPost> }) {
  if (posts.length === 0) {
    return <p className="text-muted-foreground py-12 text-center">No posts yet.</p>;
  }

  return (
    <div>
      {posts.map((post) => (
        <PostCard key={post.id} post={post} />
      ))}
    </div>
  );
}
