import * as v from "valibot";
import {
  createElectricShapeCollection,
  electricStringifiedIntegerSchema,
} from "@forge-metal/web-env";

const electricPostSchema = v.object({
  id: v.string(),
  slug: v.string(),
  title: v.string(),
  subtitle: v.string(),
  cover_image_url: v.string(),
  content: v.string(),
  author_name: v.string(),
  status: v.string(),
  published_at: v.nullable(v.string()),
  reading_time_minutes: electricStringifiedIntegerSchema,
  total_claps: electricStringifiedIntegerSchema,
  tags: v.string(),
  created_at: v.string(),
  updated_at: v.string(),
});

export type ElectricPost = v.InferOutput<typeof electricPostSchema>;

/** Published posts — used by the public reader. */
export function createPublishedPostsCollection() {
  return createElectricShapeCollection({
    id: "sync-posts-published",
    schema: electricPostSchema,
    table: "posts",
    where: "status = 'published'",
    getKey: (item) => item.id,
  });
}

/** All posts including drafts — used by the authenticated editor. */
export function createAllPostsCollection() {
  return createElectricShapeCollection({
    id: "sync-posts-all",
    schema: electricPostSchema,
    table: "posts",
    getKey: (item) => item.id,
  });
}
