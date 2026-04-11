import { createRouter } from "@tanstack/react-router";
import { QueryClient } from "@tanstack/react-query";
import { routeTree } from "./routeTree.gen";
import { anonymousAuth } from "@forge-metal/auth-web/isomorphic";

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

  return createRouter({
    routeTree,
    defaultPreload: "intent",
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
