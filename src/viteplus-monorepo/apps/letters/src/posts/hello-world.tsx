import type { Post } from "./types";

export const post: Post = {
  meta: {
    slug: "hello-world",
    title: "Hello, world",
    subtitle: "Why we're publishing in the open",
    description:
      "An introduction to Letters: a static, code-authored blog that ships with Forge Metal.",
    publishedAt: "2026-04-12",
    author: "Forge Metal",
    readingMinutes: 2,
  },
  Body: () => (
    <>
      <p>
        Letters is the simplest possible blog the team can ship: a handful of React components,
        Tailwind for typography, and nothing in front of the reader except text. No database, no
        editor, no claps, no view counter. Posts live in version control alongside the rest of the
        platform.
      </p>
      <h2>Why no CMS</h2>
      <p>
        We tried a richer setup with a Postgres-backed editor and live-syncing collections. It was a
        fun demo, and it was the wrong shape for a personal log. The ratio of moving parts to
        published words was too high. Every dependency we removed made the page faster, the build
        cleaner, and the words slightly more important.
      </p>
      <h2>What stays</h2>
      <p>
        Server-side rendering, real meta tags, an Open Graph card per post, JSON-LD for search
        engines, and a typography pass tuned for long-form reading. If you can read this in whatever
        feed reader you use, that's the contract.
      </p>
    </>
  ),
};
