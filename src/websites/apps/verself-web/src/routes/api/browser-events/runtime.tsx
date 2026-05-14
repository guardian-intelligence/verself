import { createFileRoute } from "@tanstack/react-router";
import { ingestBrowserEvent } from "~/lib/telemetry/browser-event-ingest";

export const Route = createFileRoute("/api/browser-events/runtime")({
  server: {
    handlers: {
      POST: async ({ request }) => ingestBrowserEvent(request, "browser_runtime"),
    },
  },
});
