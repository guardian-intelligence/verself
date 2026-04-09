import { createCollection } from "@tanstack/db";
import { electricCollectionOptions } from "@tanstack/electric-db-collection";

// Electric requires an absolute shape URL. Keep the real sync path same-origin
// in the browser, but return a harmless absolute fallback during SSR so the URL
// parser never sees a bare relative path.
function electricShapeURL(): string {
  if (typeof window !== "undefined" && window.location?.origin) {
    return new URL("/v1/shape", window.location.origin).toString();
  }
  return "http://127.0.0.1/v1/shape";
}

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
        url: electricShapeURL(),
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
        url: electricShapeURL(),
        params: {
          table: "posts",
        },
      },
      getKey: (item: Record<string, unknown>) => String(item.id),
    }) as any,
  );
}
