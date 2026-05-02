import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/api/sync/execution-logs/$attemptId")({
  server: {
    handlers: {
      GET: ({ request }) => proxyExecutionLogsShape(request),
      POST: ({ request }) => proxyExecutionLogsShape(request),
    },
  },
});

async function proxyExecutionLogsShape(request: Request): Promise<Response> {
  const { electricShapeDefinitions, proxyElectricShape } = await import(
    "~/server-fns/electric-proxy.server"
  );
  const attemptID = new URL(request.url).pathname.split("/").pop() ?? "";
  return proxyElectricShape(request, (snapshot) =>
    electricShapeDefinitions.executionLogs(snapshot, attemptID),
  );
}
