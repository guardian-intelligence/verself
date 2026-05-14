import { createFileRoute } from "@tanstack/react-router";
import { ingestBrowserEvent } from "~/lib/telemetry/browser-event-ingest";

export const Route = createFileRoute("/api/browser-events/reports")({
  server: {
    handlers: {
      POST: async ({ request }) => ingestBrowserEvent(request, "reporting_api"),
    },
  },
});
