import { Link, useRouterState } from "@tanstack/react-router";

// TopNav — the single masthead nav surfaced on every Guardian treatment.
// Three rooms: Home (Workshop) · Letters · Newsroom. The same component
// renders under all three treatment scopes so the chrome reads uniformly:
// same width, same items, same placement. Active-state styling resolves
// from `var(--treatment-ink)` so the indicator repaints per treatment
// (graphite on Iron, ink on Argent/Paper) without per-room logic.

interface NavItem {
  readonly to: "/" | "/letters" | "/newsroom";
  readonly label: string;
  readonly match: (pathname: string) => boolean;
}

const ITEMS: ReadonlyArray<NavItem> = [
  { to: "/", label: "Home", match: (p) => p === "/" },
  { to: "/letters", label: "Letters", match: (p) => p === "/letters" || p.startsWith("/letters/") },
  {
    to: "/newsroom",
    label: "Newsroom",
    match: (p) => p === "/newsroom" || p.startsWith("/newsroom/"),
  },
];

export function TopNav() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  return (
    <nav className="hidden items-center gap-7 md:flex">
      {ITEMS.map((item) => {
        const isActive = item.match(pathname);
        return (
          <Link
            key={item.to}
            to={item.to}
            aria-current={isActive ? "page" : undefined}
            className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] transition-colors hover:underline hover:underline-offset-4"
            style={{
              color: isActive ? "var(--treatment-ink)" : "var(--treatment-muted)",
              textDecoration: isActive ? "underline" : undefined,
              textDecorationThickness: "1px",
              textUnderlineOffset: "6px",
            }}
          >
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}
