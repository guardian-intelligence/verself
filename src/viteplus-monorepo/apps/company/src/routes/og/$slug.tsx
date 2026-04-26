import { createFileRoute } from "@tanstack/react-router";
import { ogGetResponse, ogHeadResponse } from "~/og/handler";

// Top-level OG card endpoint. Paths: /og/home, /og/design, /og/letters, etc.
// The handler logic lives in ~/og/handler so /og/letter/$slug can share it.
// Served as image/svg+xml — social platforms that demand PNG fall back to a
// pre-rendered copy under public/og/*.png in a future iteration.

export const Route = createFileRoute("/og/$slug")({
  server: {
    handlers: {
      HEAD: ({ params }) => ogHeadResponse(params.slug),
      GET: ({ params }) => ogGetResponse(params.slug),
    },
  },
});
