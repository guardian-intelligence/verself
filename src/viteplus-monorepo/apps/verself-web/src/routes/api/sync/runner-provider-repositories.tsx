import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/api/sync/runner-provider-repositories")({
  server: {
    handlers: {
      GET: ({ request }) => proxyRunnerProviderRepositoriesShape(request),
      POST: ({ request }) => proxyRunnerProviderRepositoriesShape(request),
    },
  },
});

async function proxyRunnerProviderRepositoriesShape(request: Request): Promise<Response> {
  const { electricShapeDefinitions, proxyElectricShape } = await import(
    "~/server-fns/electric-proxy.server"
  );
  return proxyElectricShape(request, electricShapeDefinitions.runnerProviderRepositories);
}
