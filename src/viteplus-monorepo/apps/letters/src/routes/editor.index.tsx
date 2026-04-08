import { createFileRoute, Link } from "@tanstack/react-router";
import { useMemo } from "react";
import { useLiveQuery } from "@tanstack/react-db";
import { createAllPostsCollection, type ElectricPost } from "~/lib/collections";

export const Route = createFileRoute("/editor/")({
  component: EditorDashboard,
});

function formatDate(dateStr: string | null): string {
  if (!dateStr) return "";
  const d = new Date(dateStr);
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });
}

function EditorDashboard() {
  const collection = useMemo(() => createAllPostsCollection(), []);
  const { data: posts } = useLiveQuery((q) => q.from({ p: collection }), [collection]);

  const sortedPosts = useMemo(() => {
    if (!posts) return [];
    return [...(posts as ElectricPost[])].sort(
      (a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
    );
  }, [posts]);

  if (sortedPosts.length === 0) {
    return (
      <div className="text-center py-12">
        <p className="text-muted-foreground mb-4">No posts yet. Start writing!</p>
        <Link
          to="/editor/new"
          className="px-4 py-2 rounded-md bg-foreground text-background hover:bg-foreground/90"
        >
          Create your first post
        </Link>
      </div>
    );
  }

  return (
    <div>
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border text-left text-muted-foreground">
            <th className="py-2 font-medium">Title</th>
            <th className="py-2 font-medium w-24">Status</th>
            <th className="py-2 font-medium w-24">Claps</th>
            <th className="py-2 font-medium w-32">Updated</th>
          </tr>
        </thead>
        <tbody>
          {sortedPosts.map((post) => (
            <tr key={post.id} className="border-b border-border hover:bg-muted/50">
              <td className="py-3">
                <Link
                  to="/editor/$slug"
                  params={{ slug: post.slug }}
                  className="font-medium hover:underline"
                >
                  {post.title || "Untitled"}
                </Link>
              </td>
              <td className="py-3">
                <span
                  className={`
                    inline-block px-2 py-0.5 rounded-full text-xs font-medium
                    ${post.status === "published" ? "bg-success/10 text-success" : "bg-muted text-muted-foreground"}
                  `}
                >
                  {post.status}
                </span>
              </td>
              <td className="py-3 text-muted-foreground tabular-nums">{post.total_claps}</td>
              <td className="py-3 text-muted-foreground">{formatDate(post.updated_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
