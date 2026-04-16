import { createRootRoute, HeadContent, Link, Outlet, Scripts } from "@tanstack/react-router";
import { type ReactNode } from "react";
import "~/styles/app.css";

export const Route = createRootRoute({
  component: RootComponent,
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "theme-color", content: "#ffffff" },
      { property: "og:site_name", content: "Forge Metal Platform" },
    ],
  }),
});

function RootComponent() {
  return (
    <RootDocument>
      <Outlet />
    </RootDocument>
  );
}

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body className="bg-background text-foreground font-sans antialiased">
        <div className="flex min-h-svh flex-col">
          <TopBar />
          <main id="main" className="flex-1">
            {children}
          </main>
        </div>
        <Scripts />
      </body>
    </html>
  );
}

// Minimal top bar — brand lockup only. Section navigation belongs to the
// page it applies to: /docs owns its own left rail for docs sections, and
// each docs page owns its own AnchorNav for in-page sections. Duplicating
// those links up here would just confuse the hierarchy.
function TopBar() {
  return (
    <header className="sticky top-0 z-30 border-b border-border bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="mx-auto flex h-[var(--header-h)] w-full max-w-7xl items-center px-4 md:px-6">
        <Link to="/" className="flex items-center gap-2 font-semibold tracking-tight">
          <span aria-hidden="true" className="text-base">
            ◼
          </span>
          <span className="text-sm">Forge Metal Platform</span>
        </Link>
      </div>
    </header>
  );
}
