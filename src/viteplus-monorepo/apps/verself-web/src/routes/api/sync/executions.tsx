import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/api/sync/executions")({
  server: {
    handlers: {
      GET: ({ request }) => proxyExecutionsShape(request),
      POST: ({ request }) => proxyExecutionsShape(request),
    },
  },
});

async function proxyExecutionsShape(request: Request): Promise<Response> {
  const { electricShapeDefinitions, proxyElectricShape } = await import(
    "~/server-fns/electric-proxy.server"
  );
  return proxyElectricShape(request, electricShapeDefinitions.executions);
}
