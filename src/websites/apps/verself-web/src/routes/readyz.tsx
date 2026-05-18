import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/readyz")({
  server: {
    handlers: {
      GET: () =>
        new Response("ok\n", {
          headers: { "content-type": "text/plain; charset=utf-8" },
        }),
    },
  },
});
