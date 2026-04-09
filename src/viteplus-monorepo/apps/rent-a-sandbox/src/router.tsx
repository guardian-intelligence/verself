import { createRouter } from "@tanstack/react-router";
import { QueryClient } from "@tanstack/react-query";
import { routeTree } from "./routeTree.gen";
import { AppNotFound, AppPending, AppRouteError } from "./components/route-boundaries";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      refetchOnWindowFocus: true,
    },
  },
});

export function getRouter() {
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
      queryClient,
    },
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof getRouter>;
  }
}
