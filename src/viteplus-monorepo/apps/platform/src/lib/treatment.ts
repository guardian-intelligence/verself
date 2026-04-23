import { useMatches } from "@tanstack/react-router";
import type { Treatment } from "@forge-metal/brand";

// Platform mirrors apps/company/src/lib/treatment.ts. Platform routes default
// to treatment="company" (the default Iron chrome) — a Platform route would
// declare staticData.treatment if a future docs section wanted a different
// treatment (e.g. a Workshop-themed onboarding, a Letters-themed postmortem).

export function useCurrentTreatment(): Treatment {
  const matches = useMatches();
  for (let i = matches.length - 1; i >= 0; i--) {
    const match = matches[i];
    const treatment = (match?.staticData as { treatment?: Treatment } | undefined)?.treatment;
    if (treatment) return treatment;
  }
  return "company";
}
