import { GitPullRequestArrow } from "lucide-react";
import type { ConsoleAppDefinition } from "../../runtime/app-types";
import { PlaceholderInsightPanel } from "../placeholder-panel";

export const ciFlightApp: ConsoleAppDefinition = {
  id: "ci-flight",
  label: "Flight Context",
  pageTitle: "For You",
  shortLabel: "Flights",
  description:
    "Live run context belongs in the activity layer; this panel will hold deeper run insights.",
  eyebrow: "CI Flight",
  icon: GitPullRequestArrow,
  accent: "green",
  access: {
    kind: "signed_in",
    signedOutTitle: "Sign in to track live CI",
    signedOutBody: "Debug fixtures can render here, but live organization runs require a session.",
  },
  metrics: [
    { label: "State", value: "On time", tone: "good" },
    { label: "Remaining", value: "12m" },
    { label: "Commits", value: "4" },
  ],
  Panel: PlaceholderInsightPanel,
};
