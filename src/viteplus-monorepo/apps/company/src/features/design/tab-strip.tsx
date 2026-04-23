import { Link } from "@tanstack/react-router";
import { emitSpan } from "~/lib/telemetry/browser";

// DesignTabStrip — four-way nav sitting just below AppChrome on every
// /design/* route. The strip is section navigation for the brand system,
// not primary site nav; the `BRAND SYSTEM` eyebrow above it makes that
// reading unambiguous.
//
// The container is horizontally scrollable on narrow viewports (375 px lands
// Overview + Workshop + half of Newsroom, inviting the gesture). Earlier
// iterations used only `overflow-x-auto`; per CSS Overflow Module Level 3 §3
// the visible axis computes to `auto` when the other axis is not `visible`,
// which produced a phantom vertical scrollbar on desktop. Pairing the
// x-auto with `overflow-y-hidden` forces the computed value and kills the
// artifact.
//
// Each tab link emits design.tab.navigate on click, carrying from/to pairs
// so the canary flow in ClickHouse can assert a specific navigation order.

type TabRoute = "/design" | "/design/workshop" | "/design/newsroom" | "/design/letters";

interface Tab {
  readonly to: TabRoute;
  readonly label: string;
}

const TABS: readonly Tab[] = [
  { to: "/design", label: "Overview" },
  { to: "/design/workshop", label: "Workshop" },
  { to: "/design/newsroom", label: "Newsroom" },
  { to: "/design/letters", label: "Letters" },
];

export interface DesignTabStripProps {
  readonly currentRoute: string;
}

export function DesignTabStrip({ currentRoute }: DesignTabStripProps) {
  return (
    <nav
      aria-label="Design system treatments"
      className="sticky top-[var(--header-h)] z-20"
      style={{
        background: "var(--treatment-ground)",
        borderBottom: "1px solid var(--treatment-hairline)",
      }}
    >
      <div className="mx-auto w-full max-w-7xl px-4 pt-3 pb-0 md:px-6">
        <p
          className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em]"
          style={{
            color: "var(--treatment-muted-faint)",
            fontVariationSettings: '"wght" 600',
            margin: 0,
          }}
        >
          Brand system
        </p>
      </div>
      <div className="mx-auto w-full max-w-7xl overflow-x-auto overflow-y-hidden px-4 md:px-6">
        <ul className="flex min-w-max items-center gap-1">
          {TABS.map((tab) => {
            const isActive = tab.to === currentRoute;
            return (
              <li key={tab.to}>
                <Link
                  to={tab.to}
                  data-active={isActive ? "true" : "false"}
                  onClick={() => {
                    emitSpan("design.tab.navigate", {
                      from_route: currentRoute,
                      to_route: tab.to,
                    });
                  }}
                  className="group inline-flex items-center whitespace-nowrap px-3 py-3 text-sm"
                  style={{
                    color: isActive ? "var(--treatment-ink)" : "var(--treatment-muted)",
                    fontFamily: "'Geist', sans-serif",
                    fontWeight: isActive ? 500 : 400,
                    borderBottom: isActive
                      ? "2px solid var(--treatment-accent)"
                      : "2px solid transparent",
                    marginBottom: "-1px",
                  }}
                >
                  {tab.label}
                </Link>
              </li>
            );
          })}
        </ul>
      </div>
    </nav>
  );
}
