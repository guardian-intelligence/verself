import type { ComponentType } from "react";
import type { LucideIcon } from "lucide-react";
import type { ConsoleSearch } from "./console-search";

export type ConsoleAccessKind = "public" | "signed_in" | "organization" | "entitled";

export type ConsoleAppPanelMetric = {
  readonly label: string;
  readonly value: string;
  readonly tone?: "neutral" | "good" | "warn" | "danger";
};

export type ConsoleAppRenderContext = {
  readonly orgSlug: string;
  readonly search: ConsoleSearch;
};

export type ConsoleAppPanelProps = {
  readonly app: ConsoleAppDefinition;
  readonly context: ConsoleAppRenderContext;
};

export type ConsoleAppDefinition = {
  readonly id: ConsoleSearch["app"];
  readonly label: string;
  readonly pageTitle?: string;
  readonly shortLabel: string;
  readonly description: string;
  readonly eyebrow: string;
  readonly icon: LucideIcon;
  readonly accent: "green" | "gold" | "blue" | "red";
  readonly access: {
    readonly kind: ConsoleAccessKind;
    readonly signedOutTitle?: string;
    readonly signedOutBody?: string;
    readonly upgradeTitle?: string;
    readonly upgradeBody?: string;
  };
  readonly metrics: readonly ConsoleAppPanelMetric[];
  readonly Panel: ComponentType<ConsoleAppPanelProps>;
};
