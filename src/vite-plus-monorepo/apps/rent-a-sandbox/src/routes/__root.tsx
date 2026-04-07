import {
  createRootRouteWithContext,
  HeadContent,
  Link,
  Outlet,
  Scripts,
} from "@tanstack/react-router";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import "~/styles/app.css";

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient;
}>()({
  component: RootComponent,
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "Rent-a-Sandbox | Vite+ Baseline" },
    ],
  }),
});

function RootComponent() {
  const { queryClient } = Route.useRouteContext();

  return (
    <QueryClientProvider client={queryClient}>
      <RootDocument>
        <Outlet />
      </RootDocument>
    </QueryClientProvider>
  );
}

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body className="min-h-screen bg-slate-950 text-slate-100 antialiased">
        <div className="relative isolate min-h-screen overflow-hidden">
          <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top,rgba(245,158,11,0.22),transparent_32%),linear-gradient(180deg,#020617_0%,#0f172a_55%,#111827_100%)]" />
          <div className="relative mx-auto flex min-h-screen w-full max-w-6xl flex-col px-6 pb-12 pt-6 lg:px-8">
            <header className="mb-12 flex items-center justify-between border-b border-white/10 pb-5">
              <Link
                to="/"
                className="text-sm font-semibold uppercase tracking-[0.28em] text-amber-200/90"
              >
                Forge Metal
              </Link>
              <span className="rounded-full border border-amber-400/30 bg-amber-500/10 px-3 py-1 text-xs uppercase tracking-[0.24em] text-amber-100/80">
                Vite+ Staging Workspace
              </span>
            </header>
            <main className="flex-1">{children}</main>
          </div>
        </div>
        <Scripts />
      </body>
    </html>
  );
}
