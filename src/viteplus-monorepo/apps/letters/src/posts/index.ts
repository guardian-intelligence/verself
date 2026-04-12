import { post as helloWorld } from "./hello-world";
import { post as firecrackerCi } from "./firecracker-ci";
import type { Post, PostMeta } from "./types";

export type { Post, PostMeta };

const ALL_POSTS: ReadonlyArray<Post> = [helloWorld, firecrackerCi];

const BY_SLUG = new Map(ALL_POSTS.map((post) => [post.meta.slug, post]));

export function listPosts(): ReadonlyArray<Post> {
  return [...ALL_POSTS].sort(
    (left, right) =>
      new Date(right.meta.publishedAt).getTime() - new Date(left.meta.publishedAt).getTime(),
  );
}

export function getPost(slug: string): Post | undefined {
  return BY_SLUG.get(slug);
}
