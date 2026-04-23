import { createFileRoute, Outlet, useMatches } from "@tanstack/react-router";
import type { Treatment } from "@forge-metal/brand";
import { DesignTabStrip } from "~/features/design/tab-strip";

// Layout for /design/*. The tab strip is rendered here so every child route
// inherits it; the strip sits immediately below AppChrome and reads the
// current treatment from the deepest matched route's staticData. The layout
// intentionally does NOT wrap the Outlet in its own data-treatment div —
// __root.tsx already sets data-treatment on the page-wide wrapper from the
// same useCurrentTreatment() hook, so the strip, the chrome, and the body
// all repaint in lockstep when the user changes tabs.

export const Route = createFileRoute("/design")({
  component: DesignLayout,
});

function DesignLayout() {
  const matches = useMatches();
  const deepest = matches[matches.length - 1];
  const currentRoute = deepest?.pathname ?? "/design";
  const currentTreatment =
    (deepest?.staticData as { treatment?: Treatment } | undefined)?.treatment ?? "company";

  return (
    <>
      <DesignTabStrip currentTreatment={currentTreatment} currentRoute={currentRoute} />
      <Outlet />
    </>
  );
}
