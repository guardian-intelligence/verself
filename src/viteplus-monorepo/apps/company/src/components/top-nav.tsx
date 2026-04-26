import { Link, useRouterState } from "@tanstack/react-router";
import { useEffect, useId, useState } from "react";
import { Menu, X } from "lucide-react";

// TopNav — the single masthead nav surfaced on every Guardian treatment.
// Three rooms: Home (Workshop) · Letters · Newsroom. The same component
// renders under all three treatment scopes so the chrome reads uniformly:
// same width, same items, same placement. Active-state styling resolves
// from `var(--treatment-ink)` so the indicator repaints per treatment
// (graphite on Iron, ink on Argent/Paper) without per-room logic.
//
// Mobile fallback is a same-tree disclosure (no Portal). Keeping the
// dialog inside the layout's data-treatment subtree means treatment
// tokens (--treatment-ground / --treatment-ink / --treatment-muted)
// cascade naturally; a portaled Sheet would re-root under <body> and
// lose the scope.

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
  const [open, setOpen] = useState(false);
  const panelId = useId();

  // Close the mobile panel when the route flips (so tapping a link
  // dismisses without us reading internal Link events).
  useEffect(() => {
    setOpen(false);
  }, [pathname]);

  // Escape closes; body scroll locks while the panel covers the page.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }, [open]);

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

      <button
        type="button"
        aria-label="Open menu"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen(true)}
        className="-mr-2 inline-flex items-center justify-center p-2 md:hidden"
        style={{ color: "var(--treatment-ink)" }}
      >
        <Menu size={22} aria-hidden="true" />
      </button>

      {open ? <MobileNavPanel id={panelId} pathname={pathname} onClose={() => setOpen(false)} /> : null}
    </>
  );
}

function MobileNavPanel({
  id,
  pathname,
  onClose,
}: {
  id: string;
  pathname: string;
  onClose: () => void;
}) {
  return (
    <div
      id={id}
      role="dialog"
      aria-modal="true"
      aria-label="Site navigation"
      className="fixed inset-0 z-50 md:hidden"
    >
      <button
        type="button"
        aria-label="Close menu"
        onClick={onClose}
        className="absolute inset-0 block h-full w-full"
        style={{ background: "rgba(11, 11, 11, 0.55)" }}
      />
      <div
        className="absolute inset-x-0 top-0 flex flex-col gap-8 px-6 py-6"
        style={{
          background: "var(--treatment-ground)",
          color: "var(--treatment-ink)",
          borderBottom:
            "var(--treatment-rule-thickness) solid var(--treatment-rule-color)",
        }}
      >
        <div className="flex items-center justify-end">
          <button
            type="button"
            aria-label="Close menu"
            onClick={onClose}
            className="-mr-2 inline-flex items-center justify-center p-2"
            style={{ color: "var(--treatment-ink)" }}
          >
            <X size={22} aria-hidden="true" />
          </button>
        </div>
        <nav className="flex flex-col gap-1 pb-2">
          {ITEMS.map((item) => {
            const isActive = item.match(pathname);
            return (
              <Link
                key={item.to}
                to={item.to}
                aria-current={isActive ? "page" : undefined}
                onClick={onClose}
                className="py-2 font-mono text-[14px] font-medium uppercase tracking-[0.16em]"
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
      </div>
    </div>
  );
}
