import { useMatches } from "@tanstack/react-router";
import type { Treatment } from "@forge-metal/brand";

// useCurrentTreatment — reads the deepest matched route's staticData.treatment
// and returns it, defaulting to "company" for any route that has not declared
// a treatment. This is how __root.tsx learns which treatment the current
// route wants to render without __root.tsx having to know every route.
//
// Routes opt into a non-default treatment by declaring:
//
//   createFileRoute("/letters/")({
//     staticData: { treatment: "letters" },
//     component: LettersIndex,
//     ...
//   });
//
// When a user navigates between treatments, useMatches() returns the new
// match tree, this hook returns the new treatment, and the surrounding
// AppChrome + data-treatment wrapper repaint via their CSS transition.

export function useCurrentTreatment(): Treatment {
  const matches = useMatches();
  for (let i = matches.length - 1; i >= 0; i--) {
    const match = matches[i];
    const treatment = (match?.staticData as { treatment?: Treatment } | undefined)?.treatment;
    if (treatment) return treatment;
  }
  return "company";
}
