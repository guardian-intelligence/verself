import { createServerFn } from "@tanstack/react-start";
import postgres from "postgres";
import { requireURLFromEnv } from "@forge-metal/web-env";
import { lettersAuthMiddleware } from "./auth";

const DATABASE_URL = requireURLFromEnv("DATABASE_URL");

function getDb() {
  return postgres(DATABASE_URL, { max: 5 });
}

function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\w\s-]/g, "")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 80);
}

function estimateReadingTime(content: unknown): number {
  // Count words in ProseMirror JSON content recursively
  let wordCount = 0;
  function walk(node: any) {
    if (!node) return;
    if (node.text) {
      wordCount += node.text.split(/\s+/).filter(Boolean).length;
    }
    if (Array.isArray(node.content)) {
      for (const child of node.content) walk(child);
    }
  }
  walk(content);
  return Math.max(1, Math.ceil(wordCount / 238));
}

export const createPost = createServerFn({ method: "POST" })
  .middleware([lettersAuthMiddleware])
  .inputValidator(
    (data: {
      title: string;
      content?: unknown;
      subtitle?: string;
      cover_image_url?: string;
      tags?: string[];
      author_name?: string;
    }) => data,
  )
  .handler(async ({ data }) => {
    const sql = getDb();
    try {
      const slug = slugify(data.title) || "untitled";
      const content = data.content ?? {};
      const readingTime = estimateReadingTime(content);
      const [post] = await sql`
        INSERT INTO posts (slug, title, subtitle, cover_image_url, content, author_name, reading_time_minutes, tags)
        VALUES (
          ${slug},
          ${data.title},
          ${data.subtitle ?? ""},
          ${data.cover_image_url ?? ""},
          ${JSON.stringify(content)},
          ${data.author_name ?? ""},
          ${readingTime},
          ${data.tags ?? []}
        )
        RETURNING id, slug
      `;
      if (!post) throw new Error("Post creation failed");
      return { id: post.id, slug: post.slug };
    } finally {
      await sql.end();
    }
  });

export const updatePost = createServerFn({ method: "POST" })
  .middleware([lettersAuthMiddleware])
  .inputValidator(
    (data: {
      slug: string;
      title?: string;
      subtitle?: string;
      cover_image_url?: string;
      content?: unknown;
      tags?: string[];
      author_name?: string;
      newSlug?: string;
    }) => data,
  )
  .handler(async ({ data }) => {
    const sql = getDb();
    try {
      const [post] = await sql`
        UPDATE posts SET
          title = COALESCE(${data.title ?? null}, title),
          subtitle = COALESCE(${data.subtitle ?? null}, subtitle),
          cover_image_url = COALESCE(${data.cover_image_url ?? null}, cover_image_url),
          content = COALESCE(${data.content ? JSON.stringify(data.content) : null}::jsonb, content),
          reading_time_minutes = COALESCE(${data.content ? estimateReadingTime(data.content) : null}, reading_time_minutes),
          tags = COALESCE(${data.tags ?? null}, tags),
          author_name = COALESCE(${data.author_name ?? null}, author_name),
          slug = COALESCE(${data.newSlug ?? null}, slug),
          updated_at = now()
        WHERE slug = ${data.slug}
        RETURNING id, slug
      `;
      if (!post) throw new Error(`Post not found: ${data.slug}`);
      return { id: post.id, slug: post.slug };
    } finally {
      await sql.end();
    }
  });

export const deletePost = createServerFn({ method: "POST" })
  .middleware([lettersAuthMiddleware])
  .inputValidator((data: { slug: string }) => data)
  .handler(async ({ data }) => {
    const sql = getDb();
    try {
      const [post] = await sql`DELETE FROM posts WHERE slug = ${data.slug} RETURNING id`;
      if (!post) throw new Error(`Post not found: ${data.slug}`);
      return { deleted: true };
    } finally {
      await sql.end();
    }
  });

export const publishPost = createServerFn({ method: "POST" })
  .middleware([lettersAuthMiddleware])
  .inputValidator((data: { slug: string }) => data)
  .handler(async ({ data }) => {
    const sql = getDb();
    try {
      const [post] = await sql`
        UPDATE posts SET status = 'published', published_at = now(), updated_at = now()
        WHERE slug = ${data.slug}
        RETURNING id, slug, published_at
      `;
      if (!post) throw new Error(`Post not found: ${data.slug}`);
      return { slug: post.slug, published_at: post.published_at };
    } finally {
      await sql.end();
    }
  });

export const unpublishPost = createServerFn({ method: "POST" })
  .middleware([lettersAuthMiddleware])
  .inputValidator((data: { slug: string }) => data)
  .handler(async ({ data }) => {
    const sql = getDb();
    try {
      const [post] = await sql`
        UPDATE posts SET status = 'draft', published_at = NULL, updated_at = now()
        WHERE slug = ${data.slug}
        RETURNING id, slug
      `;
      if (!post) throw new Error(`Post not found: ${data.slug}`);
      return { slug: post.slug };
    } finally {
      await sql.end();
    }
  });

/** Fetch a single post by slug (server-side, for SSR). */
export const getPostBySlug = createServerFn({ method: "GET" })
  .inputValidator((data: { slug: string }) => data)
  .handler(async ({ data }) => {
    const sql = getDb();
    try {
      const [post] = await sql`SELECT * FROM posts WHERE slug = ${data.slug} LIMIT 1`;
      return post ?? null;
    } finally {
      await sql.end();
    }
  });
