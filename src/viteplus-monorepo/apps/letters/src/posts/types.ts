import type { ComponentType } from "react";

export type PostMeta = {
  slug: string;
  title: string;
  subtitle: string;
  description: string;
  publishedAt: string;
  author: string;
  readingMinutes: number;
  coverImageUrl?: string;
};

export type Post = {
  meta: PostMeta;
  Body: ComponentType;
};
