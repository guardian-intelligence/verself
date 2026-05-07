import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/api/sync/notification-inbox-state")({
  server: {
    handlers: {
      GET: ({ request }) => proxyNotificationInboxStateShape(request),
      POST: ({ request }) => proxyNotificationInboxStateShape(request),
    },
  },
});

async function proxyNotificationInboxStateShape(request: Request): Promise<Response> {
  const { electricShapeDefinitions, proxyElectricShape } =
    await import("~/server-fns/electric-proxy.server");
  return proxyElectricShape(request, electricShapeDefinitions.notificationInboxState);
}
