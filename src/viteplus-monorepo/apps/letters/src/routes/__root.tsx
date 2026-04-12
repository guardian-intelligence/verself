import { createRootRoute, HeadContent, Link, Outlet, Scripts } from "@tanstack/react-router";
import { type ReactNode } from "react";
import "~/styles/app.css";

// Title / description / OG type / URL / Twitter card are deliberately emitted
// only in child routes. TanStack Router merges route-tree head arrays
// additively, so anything we set here would double up (and on article pages
// contradict) the per-page tags below.
export const Route = createRootRoute({
  component: RootComponent,
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "theme-color", content: "#ffffff" },
      { property: "og:site_name", content: "Letters" },
    ],
    links: [
      {
        rel: "stylesheet",
        href: "https://fonts.googleapis.com/css2?family=Playfair+Display:wght@400;700;900&display=swap",
      },
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
      <body className="font-sans antialiased">
        <div className="min-h-screen flex flex-col">
          <Nav />
          <main className="flex-1">{children}</main>
        </div>
        <Scripts />
      </body>
    </html>
  );
}

function Nav() {
  return (
    <nav className="border-b border-border">
      <div className="max-w-3xl mx-auto px-6 flex items-center h-14">
        <Link
          to="/"
          className="text-xl font-bold tracking-tight"
          style={{ fontFamily: "'Playfair Display', serif" }}
        >
          Letters
        </Link>
      </div>
    </nav>
  );
}
