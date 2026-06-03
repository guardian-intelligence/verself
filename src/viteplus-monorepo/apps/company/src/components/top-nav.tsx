import { Link, useRouterState } from "@tanstack/react-router";
import { useId, useState } from "react";
import { Menu } from "lucide-react";

// TopNav — the single masthead nav surfaced on every Guardian treatment.
// Three rooms: Home (Workshop) · Letters · News. The same component
// renders under all three treatment scopes so the chrome reads uniformly:
// same width, same items, same placement. Active-state styling resolves
// from `var(--treatment-ink)` so the indicator repaints per treatment
// (graphite on Iron, ink on Argent/Paper) without per-room logic.
//
// Mobile uses the platform Popover API: the menu belongs to the top layer,
// auto-dismisses on outside click/Escape, and only takes the space its links
// need instead of opening a full-screen disclosure.

interface NavItem {
  readonly to: "/" | "/letters" | "/news";
  readonly label: string;
  readonly match: (pathname: string) => boolean;
}

const ITEMS: ReadonlyArray<NavItem> = [
  { to: "/", label: "Home", match: (p) => p === "/" },
  { to: "/letters", label: "Letters", match: (p) => p === "/letters" || p.startsWith("/letters/") },
  {
    to: "/news",
    label: "News",
    match: (p) => p === "/news" || p.startsWith("/news/"),
  },
];

export function TopNav() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const [open, setOpen] = useState(false);
  const panelId = useId();

  return (
    <>
      <nav className="hidden items-center gap-7 md:flex">
        {ITEMS.map((item) => {
          const isActive = item.match(pathname);
          return (
            <Link
              key={item.to}
              to={item.to}
              aria-current={isActive ? "page" : undefined}
              className="inline-flex min-h-[var(--chrome-lockup-h)] items-center font-mono text-[11px] font-medium uppercase tracking-[0.16em] transition-colors hover:underline hover:underline-offset-4"
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

      <button
        type="button"
        aria-label="Open menu"
        aria-expanded={open}
        aria-controls={panelId}
        popoverTarget={panelId}
        popoverTargetAction="toggle"
        className="-mx-[11px] -my-[11px] inline-flex h-11 w-11 items-center justify-center md:hidden"
        style={{ color: "var(--treatment-ink)" }}
      >
        <Menu size={17} aria-hidden="true" />
      </button>

      <MobileNavPanel id={panelId} pathname={pathname} onToggle={setOpen} />
    </>
  );
}

function MobileNavPanel({
  id,
  pathname,
  onToggle,
}: {
  id: string;
  pathname: string;
  onToggle: (open: boolean) => void;
}) {
  return (
    <nav
      id={id}
      popover="auto"
      aria-label="Site navigation"
      onToggle={(event) => onToggle(event.newState === "open")}
      className="fixed inset-auto top-[calc(var(--header-h)+var(--chrome-edge-gap))] right-[var(--chrome-inline-gap)] bottom-auto left-[var(--chrome-inline-gap)] z-50 m-0 w-auto max-w-none border-0 px-4 py-4 text-right shadow-[0_12px_30px_rgba(0,0,0,0.14)] md:hidden"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
      }}
    >
      <ul className="m-0 flex list-none flex-col items-end gap-3 p-0">
        {ITEMS.map((item) => {
          const isActive = item.match(pathname);
          return (
            <li key={item.to}>
              <Link
                to={item.to}
                aria-current={isActive ? "page" : undefined}
                onClick={() => {
                  document.getElementById(id)?.hidePopover();
                }}
                className="py-2 text-right font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
                style={{
                  color: isActive ? "var(--treatment-ink)" : "var(--treatment-muted)",
                  textDecoration: isActive ? "underline" : undefined,
                  textDecorationThickness: "1px",
                  textUnderlineOffset: "6px",
                }}
              >
                {item.label}
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
