import { createFileRoute, Outlet, useMatches } from "@tanstack/react-router";
import { DesignTabStrip } from "~/features/design/tab-strip";

// Layout for /design/*. Renders the tab strip below the Workshop chrome and
// exposes the current route to it. The page itself sits inside the Workshop
// layout (iron ground); individual specimen routes render their treatment
// specimens inside that Workshop canvas without flipping the page chrome.

export const Route = createFileRoute("/_workshop/design")({
  component: DesignLayout,
});

function DesignLayout() {
  const matches = useMatches();
  const deepest = matches[matches.length - 1];
  const currentRoute = deepest?.pathname ?? "/design";

  return (
    <>
      <DesignTabStrip currentRoute={currentRoute} />
      <Outlet />
    </>
  );
}
