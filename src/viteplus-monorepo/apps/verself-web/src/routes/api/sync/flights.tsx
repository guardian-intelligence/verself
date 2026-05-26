import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/api/sync/flights")({
  server: {
    handlers: {
      GET: () => flightsSyncDisabled(),
      POST: () => flightsSyncDisabled(),
    },
  },
});

function flightsSyncDisabled(): Response {
  return new Response("flight sync disabled; console uses fixtures", {
    status: 404,
    headers: { "content-type": "text/plain; charset=utf-8" },
  });
}
