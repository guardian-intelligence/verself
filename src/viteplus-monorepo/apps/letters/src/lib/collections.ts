import { createCollection } from "@tanstack/db";
import { electricCollectionOptions } from "@tanstack/electric-db-collection";

const ELECTRIC_SHAPE_URL = "/v1/shape";

export interface ElectricPost {
  id: string;
  slug: string;
  title: string;
  subtitle: string;
  cover_image_url: string;
  content: string; // JSONB serialized as string by Electric
  author_name: string;
  status: string;
  published_at: string | null;
  reading_time_minutes: number;
  total_claps: number;
  tags: string; // TEXT[] serialized by Electric
  created_at: string;
  updated_at: string;
}

/** Published posts — used by the public reader. */
export function createPublishedPostsCollection() {
  return createCollection<ElectricPost>(
    electricCollectionOptions({
      id: "sync-posts-published",
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "posts",
          where: "status = 'published'",
        },
      },
      getKey: (item: Record<string, unknown>) => String(item.id),
    }) as any,
  );
}

/** All posts including drafts — used by the authenticated editor. */
export function createAllPostsCollection() {
  return createCollection<ElectricPost>(
    electricCollectionOptions({
      id: "sync-posts-all",
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "posts",
        },
      },
      getKey: (item: Record<string, unknown>) => String(item.id),
    }) as any,
  );
}
