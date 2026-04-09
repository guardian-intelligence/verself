import { createServerFn } from "@tanstack/react-start";
import postgres from "postgres";

const DATABASE_URL = process.env.DATABASE_URL || "postgresql://letters:letters@127.0.0.1:5432/letters";

function getDb() {
  return postgres(DATABASE_URL, { max: 5 });
}

const MAX_CLAPS_PER_SESSION = 50;

export const clapPost = createServerFn({ method: "POST" })
  .inputValidator((data: { slug: string; sessionId: string; count?: number }) => data)
  .handler(async ({ data }) => {
    const sql = getDb();
    try {
      const increment = Math.min(Math.max(data.count ?? 1, 1), 10); // 1-10 per request

      // Find the post
      const [post] = await sql`SELECT id FROM posts WHERE slug = ${data.slug}`;
      if (!post) throw new Error(`Post not found: ${data.slug}`);

      // UPSERT clap, capping at MAX_CLAPS_PER_SESSION
      const [clap] = await sql`
        INSERT INTO claps (post_id, session_id, count)
        VALUES (${post.id}, ${data.sessionId}, LEAST(${increment}, ${MAX_CLAPS_PER_SESSION}))
        ON CONFLICT (post_id, session_id)
        DO UPDATE SET
          count = LEAST(claps.count + ${increment}, ${MAX_CLAPS_PER_SESSION}),
          updated_at = now()
        RETURNING count
      `;
      if (!clap) {
        throw new Error(`Clap write failed for ${data.slug}`);
      }

      // Read updated total from the post (trigger already fired)
      const [updated] = await sql`SELECT total_claps FROM posts WHERE id = ${post.id}`;
      if (!updated) {
        throw new Error(`Clap total read failed for ${data.slug}`);
      }

      return {
        sessionCount: clap.count,
        totalClaps: updated.total_claps,
      };
    } finally {
      await sql.end();
    }
  });
