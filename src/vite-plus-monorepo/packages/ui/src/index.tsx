import { cva, type VariantProps } from "class-variance-authority";
import { clsx, type ClassValue } from "clsx";
import type { ComponentPropsWithoutRef } from "react";
import { twMerge } from "tailwind-merge";

export type BaselineCapability = {
  title: string;
  summary: string;
  verification: string;
};

export const baselineCapabilities = [
  {
    title: "Workspace Wiring",
    summary: "The root workspace owns Vite+, package catalogs, and recursive task execution.",
    verification: "Run vp check and vp run -r typecheck from the workspace root.",
  },
  {
    title: "TanStack Start Runtime",
    summary:
      "The app is already on the same Vite, Nitro, and React Query stack as the current frontend.",
    verification:
      "Run vp run @forge-metal/rent-a-sandbox#dev or vp run @forge-metal/rent-a-sandbox#build.",
  },
  {
    title: "Shared Package Surface",
    summary:
      "UI code now has a workspace home instead of being trapped inside a single app package.",
    verification: "Run vp test run to exercise the shared package exports.",
  },
] as const satisfies readonly BaselineCapability[];

export function summarizeCapabilities(capabilities: readonly BaselineCapability[]) {
  return `${capabilities.length} workspace checkpoints are live.`;
}

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

const panelVariants = cva(
  "rounded-[1.75rem] border p-6 shadow-[0_22px_60px_rgba(2,6,23,0.32)] backdrop-blur transition-colors",
  {
    variants: {
      tone: {
        default: "border-white/10 bg-slate-900/70 text-slate-100",
        accent:
          "border-amber-400/30 bg-[linear-gradient(180deg,rgba(245,158,11,0.16),rgba(15,23,42,0.92))] text-white",
      },
    },
    defaultVariants: {
      tone: "default",
    },
  },
);

type FeaturePanelProps = ComponentPropsWithoutRef<"article"> &
  VariantProps<typeof panelVariants> & {
    capability: BaselineCapability;
  };

export function FeaturePanel({ capability, className, tone, ...props }: FeaturePanelProps) {
  return (
    <article className={cn(panelVariants({ tone }), className)} {...props}>
      <p className="text-sm uppercase tracking-[0.24em] text-amber-200/75">{capability.title}</p>
      <p className="mt-4 text-lg font-medium leading-7 text-white">{capability.summary}</p>
      <p className="mt-6 border-t border-white/10 pt-4 text-sm leading-6 text-slate-300">
        {capability.verification}
      </p>
    </article>
  );
}
