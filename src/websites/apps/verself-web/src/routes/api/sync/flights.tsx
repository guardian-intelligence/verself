import { createFileRoute } from "@tanstack/react-router";

// Thin per-shape route. The proxy module is dynamically imported so its
// node:fs / secret code never reaches the client bundle.
export const Route = createFileRoute("/api/sync/flights")({
  server: {
    handlers: {
      GET: ({ request }) => proxyFlightsShape(request),
      POST: ({ request }) => proxyFlightsShape(request),
    },
  },
});

async function proxyFlightsShape(request: Request): Promise<Response> {
  const { electricShapeDefinitions, proxyElectricShape } =
    await import("~/server-fns/electric-proxy.server");
  return proxyElectricShape(request, electricShapeDefinitions.flights);
}
