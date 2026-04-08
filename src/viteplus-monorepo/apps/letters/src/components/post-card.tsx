import { Link } from "@tanstack/react-router";
import type { ElectricPost } from "~/lib/collections";

function formatDate(dateStr: string | null): string {
  if (!dateStr) return "";
  const d = new Date(dateStr);
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });
}

function parseTags(tags: string): string[] {
  if (!tags) return [];
  // Electric serializes TEXT[] as a string like {tag1,tag2}
  if (tags.startsWith("{") && tags.endsWith("}")) {
    return tags.slice(1, -1).split(",").filter(Boolean);
  }
  try {
    return JSON.parse(tags);
  } catch {
    return [];
  }
}

export function PostCard({ post }: { post: ElectricPost }) {
  const tags = parseTags(post.tags);

  return (
    <Link to="/$slug" params={{ slug: post.slug }} className="group block">
      <article className="border-b border-border py-8 first:pt-0">
        <div className="flex gap-6">
          <div className="flex-1 min-w-0">
            <h2 className="text-xl font-bold leading-tight mb-1 group-hover:text-accent-foreground/70 transition-colors">
              {post.title || "Untitled"}
            </h2>
            {post.subtitle && (
              <p className="text-muted-foreground text-base mb-3 line-clamp-2">{post.subtitle}</p>
            )}
            <div className="flex items-center gap-3 text-sm text-muted-foreground">
              <span>{formatDate(post.published_at)}</span>
              <span aria-hidden>·</span>
              <span>{post.reading_time_minutes} min read</span>
              {post.total_claps > 0 && (
                <>
                  <span aria-hidden>·</span>
                  <span>{post.total_claps} claps</span>
                </>
              )}
            </div>
            {tags.length > 0 && (
              <div className="flex gap-2 mt-3">
                {tags.slice(0, 3).map((tag) => (
                  <span
                    key={tag}
                    className="px-2.5 py-0.5 bg-muted text-muted-foreground rounded-full text-xs"
                  >
                    {tag}
                  </span>
                ))}
              </div>
            )}
          </div>
          {post.cover_image_url && (
            <div className="flex-shrink-0 w-28 h-28 rounded-md overflow-hidden">
              <img
                src={post.cover_image_url}
                alt=""
                className="w-full h-full object-cover"
                loading="lazy"
              />
            </div>
          )}
        </div>
      </article>
    </Link>
  );
}
