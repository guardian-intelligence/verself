import { createFileRoute } from "@tanstack/react-router";
import { ogGetResponse, ogHeadResponse } from "~/og/handler";

// Per-letter OG card. Paths: /og/letter/<letter-slug>. The catalog's
// ogSpecFor() resolves the "letter/" prefix back to the letter's frontmatter
// and synthesises an OGSpec on the fly — adding a letter requires no edits
// here or in the OG catalog.

export const Route = createFileRoute("/og/letter/$slug")({
  server: {
    handlers: {
      HEAD: ({ params }) => ogHeadResponse(`letter/${params.slug}`),
      GET: ({ params }) => ogGetResponse(`letter/${params.slug}`),
    },
  },
});
