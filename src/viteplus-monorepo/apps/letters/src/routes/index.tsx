import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { useLiveQuery } from "@tanstack/react-db";
import { createPublishedPostsCollection, type ElectricPost } from "~/lib/collections";
import { PostCard } from "~/components/post-card";

export const Route = createFileRoute("/")({
  component: HomePage,
  head: () => ({
    meta: [{ title: "Letters" }, { name: "description", content: "Thoughts and ideas" }],
  }),
});

function HomePage() {
  const collection = useMemo(() => createPublishedPostsCollection(), []);
  const { data: posts } = useLiveQuery((q) => q.from({ p: collection }), [collection]);

  const sortedPosts = useMemo(() => {
    if (!posts) return [];
    return [...(posts as ElectricPost[])].sort(
      (a, b) =>
        new Date(b.published_at ?? b.created_at).getTime() -
        new Date(a.published_at ?? a.created_at).getTime(),
    );
  }, [posts]);

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

      {sortedPosts.length === 0 ? (
        <p className="text-muted-foreground py-12 text-center">No posts yet.</p>
      ) : (
        <div>
          {sortedPosts.map((post) => (
            <PostCard key={post.id} post={post} />
          ))}
        </div>
      )}
    </div>
  );
}
