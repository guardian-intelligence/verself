import { createRouter } from "@tanstack/react-router";
import { QueryClient } from "@tanstack/react-query";
import { routeTree } from "./routeTree.gen";
import { AppNotFound, AppPending, AppRouteError } from "./components/route-boundaries";
import { anonymousAuth } from "@verself/auth-web/isomorphic";

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 10_000,
        refetchOnWindowFocus: true,
      },
    },
  });
}

export function getRouter() {
  const queryClient = createQueryClient();
  console.info("verself-web deploy timing probe: frontend rollout RCA measurement");

  return createRouter({
    routeTree,
    defaultPreload: "intent",
    defaultPendingComponent: AppPending,
    defaultPendingMs: 150,
    defaultPendingMinMs: 300,
    defaultErrorComponent: AppRouteError,
    defaultNotFoundComponent: AppNotFound,
    scrollRestoration: true,
    context: {
      auth: anonymousAuth,
      queryClient,
    },
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof getRouter>;
  }
}
