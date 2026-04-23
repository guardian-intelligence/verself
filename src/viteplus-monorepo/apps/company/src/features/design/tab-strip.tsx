import { Link } from "@tanstack/react-router";
import type { Treatment } from "@forge-metal/brand";
import { emitSpan } from "~/lib/telemetry/browser";

// DesignTabStrip — the five-way nav that sits just below AppChrome on every
// /design/* route. Horizontally scrollable by default so it never clips on
// narrow viewports (375px lands four tabs visible + the fifth partially,
// inviting the horizontal gesture). Active tab is signalled by an underline
// that picks up the current treatment's accent via var(--treatment-accent),
// which repaints along with the rest of the chrome when the route changes.
//
// Each tab link emits design.tab.navigate on click, carrying from/to pairs
// so the canary flow in ClickHouse can assert a specific navigation order.

type TabRoute =
  | "/design"
  | "/design/company"
  | "/design/workshop"
  | "/design/newsroom"
  | "/design/letters";

interface Tab {
  readonly to: TabRoute;
  readonly label: string;
  readonly treatment: Treatment | "overview";
}

const TABS: readonly Tab[] = [
  { to: "/design", label: "Overview", treatment: "overview" },
  { to: "/design/company", label: "Company", treatment: "company" },
  { to: "/design/workshop", label: "Workshop", treatment: "workshop" },
  { to: "/design/newsroom", label: "Newsroom", treatment: "newsroom" },
  { to: "/design/letters", label: "Letters", treatment: "letters" },
];

export interface DesignTabStripProps {
  readonly currentTreatment: Treatment | "overview";
  readonly currentRoute: string;
}

export function DesignTabStrip({ currentTreatment, currentRoute }: DesignTabStripProps) {
  return (
    <nav
      aria-label="Design system treatments"
      className="sticky top-[var(--header-h)] z-20 transition-colors duration-300 ease-out"
      style={{
        background: "var(--treatment-ground)",
        borderBottom: "1px solid var(--treatment-hairline)",
      }}
    >
      <div className="mx-auto w-full max-w-7xl overflow-x-auto px-4 md:px-6">
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
                      from_treatment: String(currentTreatment),
                      to_treatment: String(tab.treatment),
                    });
                  }}
                  className="group inline-flex items-center whitespace-nowrap px-3 py-3 text-sm transition-colors"
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
