import { Link } from "@tanstack/react-router";
import { emitSpan } from "~/lib/telemetry/browser";

// DesignTabStrip — four-way nav sitting just below AppChrome on every
// /design/* route. The strip is section navigation for the brand system,
// not primary site nav; the `BRAND SYSTEM` eyebrow above it makes that
// reading unambiguous.
//
// The active tab's underline paints in that tab's treatment accent (Amber
// for Workshop, Flare for Newsroom, Bordeaux for Letters) — NOT the ambient
// chrome's accent. The strip sits under the Workshop layout's iron chrome,
// so without a scope override `var(--treatment-accent)` would resolve to
// Workshop's Amber on every active tab. Per-tab accent lookup restores the
// "this room paints its own nav" teaching gesture.
//
// The container is horizontally scrollable on narrow viewports (375 px lands
// Overview + Workshop + half of Newsroom, inviting the gesture). Per CSS
// Overflow Module Level 3 §3 the visible axis computes to `auto` when the
// other axis is not `visible`, which used to produce a phantom vertical
// scrollbar on desktop. Pairing overflow-x-auto with overflow-y-hidden
// forces the computed value and kills the artifact.

type TabRoute = "/design" | "/design/workshop" | "/design/newsroom" | "/design/letters";
type TabTreatment = "workshop" | "newsroom" | "letters";

interface Tab {
  readonly to: TabRoute;
  readonly label: string;
  readonly treatment: TabTreatment;
}

const TABS: readonly Tab[] = [
  { to: "/design", label: "Overview", treatment: "workshop" },
  { to: "/design/workshop", label: "Workshop", treatment: "workshop" },
  { to: "/design/newsroom", label: "Newsroom", treatment: "newsroom" },
  { to: "/design/letters", label: "Letters", treatment: "letters" },
];

// Accent per room. Kept inline rather than reading var(--treatment-accent)
// because the strip itself sits inside the Workshop chrome scope; its job
// is to show EACH tab's accent, not the host chrome's.
const TREATMENT_ACCENT: Record<TabTreatment, string> = {
  workshop: "var(--color-amber)",
  newsroom: "var(--color-flare)",
  letters: "var(--color-bordeaux)",
};

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
            const accent = TREATMENT_ACCENT[tab.treatment];
            return (
              <li key={tab.to}>
                <Link
                  to={tab.to}
                  data-active={isActive ? "true" : "false"}
                  data-treatment={tab.treatment}
                  onClick={() => {
                    emitSpan("design.tab.navigate", {
                      from_route: currentRoute,
                      to_route: tab.to,
                      to_treatment: tab.treatment,
                    });
                  }}
                  className="group inline-flex items-center whitespace-nowrap px-3 py-3 text-sm"
                  style={{
                    color: isActive ? "var(--treatment-ink)" : "var(--treatment-muted)",
                    fontFamily: "'Geist', sans-serif",
                    fontWeight: isActive ? 500 : 400,
                    borderBottom: isActive ? `2px solid ${accent}` : "2px solid transparent",
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
