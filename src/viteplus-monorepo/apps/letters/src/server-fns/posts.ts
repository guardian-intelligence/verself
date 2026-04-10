import { createServerFn } from "@tanstack/react-start";
import { lettersAuthMiddleware } from "./auth";
import {
  createPostInputSchema,
  emptyPostContent,
  type JsonObject,
  type JsonValue,
  postOnlySlugInputSchema,
  updatePostInputSchema,
  withLettersDb,
} from "./validation";

type LettersSql = Parameters<typeof withLettersDb>[0] extends (sql: infer T) => unknown ? T : never;

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
  function isJsonObject(value: JsonValue): value is JsonObject {
    return typeof value === "object" && value !== null && !Array.isArray(value);
  }

  function walk(node: JsonValue): void {
    if (Array.isArray(node)) {
      for (const child of node) walk(child);
      return;
    }
    if (!node) return;
    if (!isJsonObject(node)) return;
    if (typeof node.text === "string") {
      wordCount += node.text.split(/\s+/).filter(Boolean).length;
    }
    if (Array.isArray(node.content)) {
      for (const child of node.content) walk(child);
    }
  }

  if (typeof content === "string" || typeof content === "number" || typeof content === "boolean") {
    const scalarWordCount = String(content).split(/\s+/).filter(Boolean).length;
    return Math.max(1, Math.ceil(scalarWordCount / 238));
  }
  if (content !== null && content !== undefined) {
    walk(content as JsonValue);
  }
  return Math.max(1, Math.ceil(wordCount / 238));
}

async function selectPostBySlug(sql: LettersSql, slug: string) {
  const [post] = await sql`
    SELECT
      id,
      slug,
      title,
      subtitle,
      cover_image_url,
      content::text AS content,
      author_name,
      status,
      published_at,
      reading_time_minutes,
      total_claps,
      tags::text AS tags,
      created_at,
      updated_at
    FROM posts
    WHERE slug = ${slug}
    LIMIT 1
  `;
  return post;
}

export const createPost = createServerFn({ method: "POST" })
  .middleware([lettersAuthMiddleware])
  .inputValidator(createPostInputSchema)
  .handler(async ({ data }) =>
    withLettersDb(async (sql) => {
      const slug = slugify(data.title) || "untitled";
      const content = data.content ?? emptyPostContent;
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
    }),
  );

export const updatePost = createServerFn({ method: "POST" })
  .middleware([lettersAuthMiddleware])
  .inputValidator(updatePostInputSchema)
  .handler(async ({ data }) =>
    withLettersDb(async (sql) => {
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
    }),
  );

export const deletePost = createServerFn({ method: "POST" })
  .middleware([lettersAuthMiddleware])
  .inputValidator(postOnlySlugInputSchema)
  .handler(async ({ data }) =>
    withLettersDb(async (sql) => {
      const [post] = await sql`DELETE FROM posts WHERE slug = ${data.slug} RETURNING id`;
      if (!post) throw new Error(`Post not found: ${data.slug}`);
      return { deleted: true };
    }),
  );

export const publishPost = createServerFn({ method: "POST" })
  .middleware([lettersAuthMiddleware])
  .inputValidator(postOnlySlugInputSchema)
  .handler(async ({ data }) =>
    withLettersDb(async (sql) => {
      const [post] = await sql`
        UPDATE posts SET status = 'published', published_at = now(), updated_at = now()
        WHERE slug = ${data.slug}
        RETURNING id, slug, published_at
      `;
      if (!post) throw new Error(`Post not found: ${data.slug}`);
      return { slug: post.slug, published_at: post.published_at };
    }),
  );

export const unpublishPost = createServerFn({ method: "POST" })
  .middleware([lettersAuthMiddleware])
  .inputValidator(postOnlySlugInputSchema)
  .handler(async ({ data }) =>
    withLettersDb(async (sql) => {
      const [post] = await sql`
        UPDATE posts SET status = 'draft', published_at = NULL, updated_at = now()
        WHERE slug = ${data.slug}
        RETURNING id, slug
      `;
      if (!post) throw new Error(`Post not found: ${data.slug}`);
      return { slug: post.slug };
    }),
  );

/** Fetch all published posts for SSR, normalized to the Electric shape. */
export const listPublishedPosts = createServerFn({ method: "GET" }).handler(async () =>
  withLettersDb(
    async (sql) =>
      sql`
      SELECT
        id,
        slug,
        title,
        subtitle,
        cover_image_url,
        content::text AS content,
        author_name,
        status,
        published_at,
        reading_time_minutes,
        total_claps,
        tags::text AS tags,
        created_at,
        updated_at
      FROM posts
      WHERE status = 'published'
      ORDER BY COALESCE(published_at, created_at) DESC
    `,
  ),
);

/** Fetch all posts for the authenticated editor, normalized to the Electric shape. */
export const listAllPosts = createServerFn({ method: "GET" })
  .middleware([lettersAuthMiddleware])
  .handler(async () =>
    withLettersDb(
      async (sql) =>
        sql`
        SELECT
          id,
          slug,
          title,
          subtitle,
          cover_image_url,
          content::text AS content,
          author_name,
          status,
          published_at,
          reading_time_minutes,
          total_claps,
          tags::text AS tags,
          created_at,
          updated_at
        FROM posts
        ORDER BY updated_at DESC
      `,
    ),
  );

/** Fetch a single post by slug (server-side, for SSR). */
export const getPostBySlug = createServerFn({ method: "GET" })
  .inputValidator(postOnlySlugInputSchema)
  .handler(async ({ data }) =>
    withLettersDb(async (sql) => {
      const post = await selectPostBySlug(sql, data.slug);
      // SSR server-function plumbing treats bare null as a middleware payload.
      return post ?? undefined;
    }),
  );
