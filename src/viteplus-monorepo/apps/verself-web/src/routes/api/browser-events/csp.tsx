import { createFileRoute } from "@tanstack/react-router";
import { ingestBrowserEvent } from "~/lib/telemetry/browser-event-ingest";

export const Route = createFileRoute("/api/browser-events/csp")({
  server: {
    handlers: {
      POST: async ({ request }) => ingestBrowserEvent(request, "csp_report_uri"),
    },
  },
});
