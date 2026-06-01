import { BellRing, Boxes, CircleGauge, CreditCard, ShieldCheck } from "lucide-react";
import type { ConsoleAppDefinition } from "./app-types";
import type { ConsoleAppId } from "./console-search";
import { ciFlightApp } from "../apps/ci-flight/app";
import { PlaceholderInsightPanel } from "../apps/placeholder-panel";

const placeholderApps: readonly ConsoleAppDefinition[] = [
  {
    id: "golden-environments",
    label: "Golden Environments",
    shortLabel: "Goldens",
    description: "Snapshot readiness and durable generation health.",
    eyebrow: "Snapshot",
    icon: Boxes,
    accent: "gold",
    access: {
      kind: "signed_in",
      signedOutTitle: "Sign in to inspect snapshots",
      signedOutBody: "Golden environment state is scoped to your organization.",
    },
    metrics: [
      { label: "Ready", value: "3", tone: "good" },
      { label: "Warming", value: "1", tone: "warn" },
      { label: "Oldest", value: "42m" },
    ],
    Panel: PlaceholderInsightPanel,
  },
  {
    id: "cache-health",
    label: "Cache Health",
    shortLabel: "Cache",
    description: "Artifact warmth, reuse, and eviction pressure.",
    eyebrow: "Artifacts",
    icon: CircleGauge,
    accent: "green",
    access: {
      kind: "signed_in",
      signedOutTitle: "Sign in to see cache warmth",
      signedOutBody: "Cache health is computed from your own repositories and runs.",
    },
    metrics: [
      { label: "Hit rate", value: "94%", tone: "good" },
      { label: "Warm paths", value: "18" },
      { label: "Pressure", value: "Low", tone: "good" },
    ],
    Panel: PlaceholderInsightPanel,
  },
  {
    id: "runner-capacity",
    label: "Runner Capacity",
    shortLabel: "Runners",
    description: "Bare-metal cell capacity and queue pressure.",
    eyebrow: "ASH",
    icon: BellRing,
    accent: "blue",
    access: {
      kind: "entitled",
      upgradeTitle: "Runner capacity requires an active plan",
      upgradeBody: "Upgrade to inspect live cell pressure and scheduling headroom.",
    },
    metrics: [
      { label: "Queued", value: "0", tone: "good" },
      { label: "Idle", value: "7" },
      { label: "Cell", value: "ASH" },
    ],
    Panel: PlaceholderInsightPanel,
  },
  {
    id: "account-security",
    label: "Account Security",
    shortLabel: "Security",
    description: "Session, device, and account posture.",
    eyebrow: "Identity",
    icon: ShieldCheck,
    accent: "red",
    access: {
      kind: "signed_in",
      signedOutTitle: "Sign in to manage account security",
      signedOutBody: "Device sessions and account controls require an authenticated session.",
    },
    metrics: [
      { label: "Devices", value: "2" },
      { label: "Risk", value: "Low", tone: "good" },
      { label: "Billing", value: "OK", tone: "good" },
    ],
    Panel: PlaceholderInsightPanel,
  },
];

export const consoleApps: readonly ConsoleAppDefinition[] = [ciFlightApp, ...placeholderApps];

export function getConsoleApp(id: ConsoleAppId): ConsoleAppDefinition {
  return consoleApps.find((app) => app.id === id) ?? ciFlightApp;
}

export function adjacentConsoleApps(id: ConsoleAppId): {
  readonly previous: ConsoleAppDefinition;
  readonly next: ConsoleAppDefinition;
} {
  const index = Math.max(
    0,
    consoleApps.findIndex((app) => app.id === id),
  );
  const previous =
    consoleApps[(index - 1 + consoleApps.length) % consoleApps.length] ?? ciFlightApp;
  const next = consoleApps[(index + 1) % consoleApps.length] ?? ciFlightApp;
  return { previous, next };
}

export const billingAppHint = {
  icon: CreditCard,
  label: "Billing",
};
