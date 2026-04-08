export const keys = {
  user: () => ["auth", "user"] as const,
  posts: () => ["posts"] as const,
  post: (slug: string) => ["posts", slug] as const,
};
