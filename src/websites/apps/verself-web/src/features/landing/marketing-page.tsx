import { Link } from "@tanstack/react-router";
import {
  ArrowRight,
  Bot,
  Check,
  ChevronRight,
  Cpu,
  DollarSign,
  Gauge,
  GitPullRequest,
  MessageCircle,
  Sparkles,
  Timer,
  Workflow,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { type CSSProperties, useReducer } from "react";

import { cn } from "@verself/ui/lib/utils";

type ProviderID = "github" | "blacksmith" | "verself";

interface Provider {
  readonly id: ProviderID;
  readonly name: string;
  readonly icon: LucideIcon;
  readonly ratePerMinute: number;
  readonly freeMinutes: number;
  readonly runtimeFactor: number;
  readonly accent: string;
  readonly surface: string;
  readonly note: string;
}

interface ProviderEstimate extends Provider {
  readonly runtimeMinutes: number;
  readonly billableMinutes: number;
  readonly monthlyCost: number;
  readonly timeSavedMinutes: number;
  readonly dollarsSaved: number;
}

interface CalculatorState {
  readonly githubMinutes: number;
}

type CalculatorAction = { readonly type: "set-minutes"; readonly value: number };

const GITHUB_MINUTES_MIN = 1_000;
const GITHUB_MINUTES_MAX = 240_000;
const GITHUB_MINUTES_STEP = 1_000;
const DEFAULT_GITHUB_MINUTES = 42_000;

const GITHUB_RATE_PER_MINUTE = 0.006;
const BLACKSMITH_RATE_PER_MINUTE = GITHUB_RATE_PER_MINUTE / 2;
const VERSELF_RATE_PER_MINUTE = GITHUB_RATE_PER_MINUTE / 2;
const VERSELF_STANDARD_FREE_MINUTES = 1_500;
const VERSELF_PROMO_FREE_MINUTES = 3_000;

const BENCHMARK_MINUTES = {
  github: 20,
  blacksmith: 2 + 10 / 60,
  verself: 1.5,
} as const;

const PROVIDERS: ReadonlyArray<Provider> = [
  {
    id: "github",
    name: "GitHub Actions",
    icon: GitPullRequest,
    ratePerMinute: GITHUB_RATE_PER_MINUTE,
    freeMinutes: 0,
    runtimeFactor: 1,
    accent: "text-stone-950",
    surface: "bg-white/70",
    note: "Current paid Linux x64 2-core minutes.",
  },
  {
    id: "blacksmith",
    name: "Blacksmith",
    icon: Gauge,
    ratePerMinute: BLACKSMITH_RATE_PER_MINUTE,
    freeMinutes: 3_000,
    runtimeFactor: BENCHMARK_MINUTES.blacksmith / BENCHMARK_MINUTES.github,
    accent: "text-emerald-800",
    surface: "bg-emerald-50/70",
    note: "Half GitHub's minute price with 3,000 free x64 2-vCPU minutes.",
  },
  {
    id: "verself",
    name: "Verself",
    icon: Zap,
    ratePerMinute: VERSELF_RATE_PER_MINUTE,
    freeMinutes: VERSELF_PROMO_FREE_MINUTES,
    runtimeFactor: BENCHMARK_MINUTES.verself / BENCHMARK_MINUTES.github,
    accent: "text-sky-900",
    surface: "bg-sky-50/80",
    note: "Launch promotion doubles the free tier from 1,500 to 3,000 minutes.",
  },
] as const;

const SETUP_ACTIONS: ReadonlyArray<{
  readonly label: string;
  readonly icon: LucideIcon;
}> = [
  { label: "Claude", icon: Sparkles },
  { label: "ChatGPT", icon: MessageCircle },
  { label: "Gemini", icon: Bot },
] as const;

const PROOF_POINTS: ReadonlyArray<{
  readonly label: string;
  readonly value: string;
  readonly icon: LucideIcon;
}> = [
  { label: "GitHub baseline", value: "20m", icon: Timer },
  { label: "Verself internal CI", value: "1m 30s", icon: Zap },
  { label: "Warm state", value: "ZFS", icon: Cpu },
] as const;

const integerFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 0,
});

const compactFormatter = new Intl.NumberFormat("en-US", {
  notation: "compact",
  maximumFractionDigits: 1,
});

const currencyFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 0,
});

const preciseCurrencyFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

function clampMinutes(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_GITHUB_MINUTES;
  return Math.min(GITHUB_MINUTES_MAX, Math.max(GITHUB_MINUTES_MIN, value));
}

function calculatorReducer(state: CalculatorState, action: CalculatorAction): CalculatorState {
  switch (action.type) {
    case "set-minutes":
      return { ...state, githubMinutes: clampMinutes(action.value) };
    default:
      return state;
  }
}

function estimateProviders(githubMinutes: number): ReadonlyArray<ProviderEstimate> {
  const githubCost = githubMinutes * GITHUB_RATE_PER_MINUTE;
  return PROVIDERS.map((provider) => {
    const runtimeMinutes = githubMinutes * provider.runtimeFactor;
    const billableMinutes = Math.max(0, runtimeMinutes - provider.freeMinutes);
    const monthlyCost = billableMinutes * provider.ratePerMinute;
    return {
      ...provider,
      runtimeMinutes,
      billableMinutes,
      monthlyCost,
      timeSavedMinutes: Math.max(0, githubMinutes - runtimeMinutes),
      dollarsSaved: Math.max(0, githubCost - monthlyCost),
    };
  });
}

function formatCurrency(value: number): string {
  if (Math.abs(value) < 100) return preciseCurrencyFormatter.format(value);
  return currencyFormatter.format(value);
}

function formatMinutes(value: number): string {
  if (value >= 100_000) return compactFormatter.format(value);
  return integerFormatter.format(Math.round(value));
}

function speedLabel(provider: Provider): string {
  if (provider.id === "github") return "1.0x";
  return `${(1 / provider.runtimeFactor).toFixed(1)}x`;
}

function sliderFill(value: number): number {
  return ((value - GITHUB_MINUTES_MIN) / (GITHUB_MINUTES_MAX - GITHUB_MINUTES_MIN)) * 100;
}

function sliderStyle(value: number): CSSProperties {
  const fill = sliderFill(value);
  return {
    background: `linear-gradient(90deg, #111827 0%, #111827 ${fill}%, #e7e2d8 ${fill}%, #e7e2d8 100%)`,
  };
}

export function MarketingLandingPage() {
  const [state, dispatch] = useReducer(calculatorReducer, {
    githubMinutes: DEFAULT_GITHUB_MINUTES,
  });
  const estimates = estimateProviders(state.githubMinutes);
  const github = estimates.find((provider) => provider.id === "github");
  const verself = estimates.find((provider) => provider.id === "verself");

  if (!github || !verself) {
    throw new Error("marketing calculator provider estimates are incomplete");
  }

  return (
    <main className="relative isolate min-h-screen overflow-hidden bg-[#f7f2e8] text-stone-950">
      <div
        aria-hidden="true"
        className="absolute inset-0 -z-20 bg-[linear-gradient(135deg,#fbf8f1_0%,#e7f4ee_34%,#dcecf2_68%,#f6e7d5_100%)]"
      />
      <div
        aria-hidden="true"
        className="absolute inset-x-0 top-0 -z-10 h-[520px] bg-[linear-gradient(180deg,rgba(255,255,255,0.78)_0%,rgba(255,255,255,0.18)_58%,rgba(255,255,255,0)_100%)]"
      />

      <TopNavigation />

      <div className="mx-auto grid w-full max-w-7xl grid-cols-1 gap-8 px-4 pb-14 pt-10 sm:px-6 md:pt-16 lg:grid-cols-[0.9fr_1.1fr] lg:gap-12 lg:px-8">
        <section className="flex flex-col justify-center">
          <div className="inline-flex w-fit items-center gap-2 rounded-md border border-white/80 bg-white/65 px-3 py-2 text-xs font-semibold text-stone-700 shadow-[0_10px_28px_-22px_rgba(20,24,31,0.65)] backdrop-blur">
            <span className="size-2 rounded-full bg-emerald-500" />
            Free tier doubled to {integerFormatter.format(VERSELF_PROMO_FREE_MINUTES)} build minutes
          </div>

          <h1 className="mt-7 max-w-3xl text-5xl font-semibold leading-[1.02] tracking-[0] text-stone-950 sm:text-6xl lg:text-7xl">
            Verself CI runners
          </h1>
          <p className="mt-5 max-w-xl text-base leading-7 text-stone-700 sm:text-lg sm:leading-8">
            GitHub Actions on bare metal Firecracker VMs, starting from the last green build
            snapshot instead of rebuilding the world on every pull request.
          </p>

          <div className="mt-7 hidden max-w-xl grid-cols-3 gap-2 sm:grid">
            {PROOF_POINTS.map((point) => (
              <div
                key={point.label}
                className="rounded-lg border border-white/70 bg-white/58 px-3 py-3 shadow-[0_1px_1px_rgba(20,24,31,0.04),0_14px_38px_-30px_rgba(20,24,31,0.5)] backdrop-blur"
              >
                <point.icon className="mb-2 size-4 text-stone-600" aria-hidden="true" />
                <div className="text-lg font-semibold leading-5 text-stone-950">{point.value}</div>
                <div className="mt-1 text-[0.72rem] font-medium leading-4 text-stone-600">
                  {point.label}
                </div>
              </div>
            ))}
          </div>

          <AgentSetupCTA className="mt-9 hidden lg:block" />
        </section>

        <SavingsCalculator
          github={github}
          estimates={estimates}
          githubMinutes={state.githubMinutes}
          onMinutesChange={(value) => dispatch({ type: "set-minutes", value })}
          verself={verself}
        />

        <AgentSetupCTA className="lg:hidden" />
      </div>

      <section className="mx-auto grid max-w-7xl grid-cols-1 gap-4 px-4 pb-16 sm:px-6 md:grid-cols-3 lg:px-8">
        <FeaturePillar
          icon={Workflow}
          title="Checkout becomes a patch"
          copy="Start from the target branch golden zvol, then apply the pull request tree instead of cloning and rebuilding cold state."
        />
        <FeaturePillar
          icon={Check}
          title="Green builds promote"
          copy="A red run cannot poison the golden image. The last trusted workspace remains authoritative until the next green target-branch run."
        />
        <FeaturePillar
          icon={Cpu}
          title="Durable mounts stay warm"
          copy="Repo setup, local services, Docker layers, and generated artifacts can live outside the workspace and still follow branch trust."
        />
      </section>
    </main>
  );
}

function AgentSetupCTA({ className }: { readonly className?: string }) {
  return (
    <div className={cn("max-w-xl", className)}>
      <div className="text-sm font-semibold text-stone-950">
        Your agents can set it up in under 5 minutes.
      </div>
      <div className="mt-3 flex flex-wrap gap-2">
        {SETUP_ACTIONS.map((action) => (
          <button
            key={action.label}
            type="button"
            aria-disabled="true"
            disabled
            className="inline-flex h-10 items-center gap-2 rounded-md border border-stone-950/10 bg-white/72 px-3 text-sm font-semibold text-stone-950 shadow-[inset_0_1px_0_rgba(255,255,255,0.85),0_10px_24px_-20px_rgba(20,24,31,0.8)] backdrop-blur transition-colors disabled:cursor-default"
          >
            <action.icon className="size-4 text-stone-600" aria-hidden="true" />
            Set up with {action.label}
            <span className="rounded-sm bg-stone-950/7 px-1.5 py-0.5 text-[0.62rem] font-semibold text-stone-600">
              Soon
            </span>
          </button>
        ))}
      </div>
      <p className="mt-3 text-xs leading-5 text-stone-600">
        Sign in is required before production runner installation. The agent setup buttons are
        staged but not connected yet.
      </p>
    </div>
  );
}

function TopNavigation() {
  return (
    <header className="relative z-10 mx-auto flex h-16 w-full max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8">
      <Link
        to="/"
        className="inline-flex items-center gap-2 text-sm font-semibold text-stone-950"
        aria-label="Verself"
      >
        <span className="grid size-7 place-items-center rounded-md bg-stone-950 text-white shadow-[0_10px_28px_-18px_rgba(20,24,31,0.8)]">
          ▽
        </span>
        Verself
      </Link>
      <Link
        to="/login"
        search={{ redirect: undefined }}
        className="inline-flex h-9 items-center gap-2 rounded-md border border-stone-950/10 bg-white/62 px-3 text-sm font-semibold text-stone-950 shadow-[inset_0_1px_0_rgba(255,255,255,0.8),0_10px_24px_-20px_rgba(20,24,31,0.8)] backdrop-blur transition-colors hover:bg-white"
      >
        Sign in
        <ChevronRight className="size-4 text-stone-500" aria-hidden="true" />
      </Link>
    </header>
  );
}

function SavingsCalculator({
  estimates,
  github,
  githubMinutes,
  onMinutesChange,
  verself,
}: {
  readonly estimates: ReadonlyArray<ProviderEstimate>;
  readonly github: ProviderEstimate;
  readonly githubMinutes: number;
  readonly onMinutesChange: (value: number) => void;
  readonly verself: ProviderEstimate;
}) {
  const monthlyHoursReturned = verself.timeSavedMinutes / 60;
  return (
    <section
      aria-label="CI savings calculator"
      className="rounded-lg border border-white/70 bg-white/66 p-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.9),0_2px_8px_rgba(20,24,31,0.05),0_36px_110px_-58px_rgba(20,24,31,0.58)] backdrop-blur-xl sm:p-4"
    >
      <div className="rounded-md border border-stone-950/8 bg-[#fbfaf6] shadow-[inset_0_1px_0_rgba(255,255,255,0.9)]">
        <div className="flex flex-col gap-5 border-b border-stone-950/8 p-4 sm:p-5">
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="text-sm font-semibold text-stone-950">Cost savings calculator</div>
              <p className="mt-1 text-xs leading-5 text-stone-600">
                Estimate from paid Linux x64 2-core GitHub Actions minutes.
              </p>
            </div>
            <div className="rounded-md bg-stone-950 px-3 py-2 text-right text-white shadow-[0_14px_28px_-18px_rgba(20,24,31,0.75)]">
              <div className="text-[0.68rem] font-medium text-white/60">Save</div>
              <div className="text-lg font-semibold leading-5">
                {formatCurrency(verself.dollarsSaved)}
              </div>
            </div>
          </div>

          <label className="block">
            <span className="flex items-center justify-between gap-3 text-sm font-medium text-stone-700">
              <span>Monthly GitHub Actions minutes</span>
              <span className="rounded-md bg-white px-2.5 py-1 text-sm font-semibold text-stone-950 shadow-[inset_0_0_0_1px_rgba(20,24,31,0.08)]">
                {integerFormatter.format(githubMinutes)}
              </span>
            </span>
            <input
              aria-label="Monthly GitHub Actions minutes"
              className="mt-4 h-2 w-full cursor-pointer appearance-none rounded-full outline-none [&::-moz-range-thumb]:size-7 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border-4 [&::-moz-range-thumb]:border-white [&::-moz-range-thumb]:bg-stone-950 [&::-moz-range-thumb]:shadow-[0_8px_20px_-10px_rgba(20,24,31,0.95)] [&::-webkit-slider-thumb]:size-7 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:border-4 [&::-webkit-slider-thumb]:border-white [&::-webkit-slider-thumb]:bg-stone-950 [&::-webkit-slider-thumb]:shadow-[0_8px_20px_-10px_rgba(20,24,31,0.95)]"
              max={GITHUB_MINUTES_MAX}
              min={GITHUB_MINUTES_MIN}
              onChange={(event) => onMinutesChange(event.currentTarget.valueAsNumber)}
              step={GITHUB_MINUTES_STEP}
              style={sliderStyle(githubMinutes)}
              type="range"
              value={githubMinutes}
            />
          </label>

          <div className="grid grid-cols-3 gap-2">
            <MetricTile
              icon={Timer}
              label="Hours returned"
              value={integerFormatter.format(Math.round(monthlyHoursReturned))}
            />
            <MetricTile
              icon={DollarSign}
              label="GitHub spend"
              value={formatCurrency(github.monthlyCost)}
            />
            <MetricTile
              icon={Zap}
              label="Verself bill"
              value={formatCurrency(verself.monthlyCost)}
            />
          </div>
        </div>

        <div className="divide-y divide-stone-950/8">
          {estimates.map((estimate) => (
            <ProviderRow key={estimate.id} estimate={estimate} githubCost={github.monthlyCost} />
          ))}
        </div>

        <div className="grid gap-3 border-t border-stone-950/8 p-4 text-xs leading-5 text-stone-600 sm:grid-cols-[1fr_auto] sm:p-5">
          <p>
            Pricing uses public May 2026 list rates for Linux x64 2-core runners. Time estimates use
            the current Verself benchmark set: GitHub 20m, Blacksmith 2m10s, Verself 1m30s.
          </p>
          <div className="flex items-center gap-2 rounded-md bg-emerald-50 px-3 py-2 text-emerald-900 shadow-[inset_0_0_0_1px_rgba(6,95,70,0.08)]">
            <Sparkles className="size-4" aria-hidden="true" />
            {integerFormatter.format(VERSELF_STANDARD_FREE_MINUTES)} to{" "}
            {integerFormatter.format(VERSELF_PROMO_FREE_MINUTES)} free
          </div>
        </div>
      </div>
    </section>
  );
}

function ProviderRow({
  estimate,
  githubCost,
}: {
  readonly estimate: ProviderEstimate;
  readonly githubCost: number;
}) {
  const isVerself = estimate.id === "verself";
  return (
    <div
      className={cn(
        "grid grid-cols-[1fr_auto] gap-3 px-4 py-4 sm:grid-cols-[1.05fr_0.75fr_0.75fr_0.75fr] sm:items-center sm:px-5",
        isVerself ? "bg-sky-50/70" : estimate.surface,
      )}
    >
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span
            className={cn(
              "grid size-8 shrink-0 place-items-center rounded-md bg-white shadow-[inset_0_0_0_1px_rgba(20,24,31,0.08)]",
              estimate.accent,
            )}
          >
            <estimate.icon className="size-4" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold text-stone-950">{estimate.name}</div>
            <div className="truncate text-xs text-stone-600">{estimate.note}</div>
          </div>
        </div>
      </div>

      <ProviderStat label="Runtime" value={`${formatMinutes(estimate.runtimeMinutes)} min`} />
      <ProviderStat
        label="Monthly bill"
        value={formatCurrency(estimate.monthlyCost)}
        emphasis={isVerself}
      />
      <div className="col-span-2 grid grid-cols-2 gap-2 sm:col-span-1 sm:block">
        <ProviderStat
          label="Saved"
          value={
            estimate.id === "github" ? "$0" : formatCurrency(githubCost - estimate.monthlyCost)
          }
          emphasis={isVerself}
        />
        <div className="mt-0 flex items-center justify-end gap-1 text-xs font-semibold text-stone-600 sm:mt-1 sm:justify-start">
          <ArrowRight className="size-3.5" aria-hidden="true" />
          {speedLabel(estimate)} speed
        </div>
      </div>
    </div>
  );
}

function ProviderStat({
  emphasis = false,
  label,
  value,
}: {
  readonly emphasis?: boolean;
  readonly label: string;
  readonly value: string;
}) {
  return (
    <div className="text-right sm:text-left">
      <div className="text-[0.68rem] font-medium text-stone-500">{label}</div>
      <div
        className={cn(
          "mt-0.5 text-sm font-semibold tabular-nums text-stone-950",
          emphasis ? "text-sky-950" : "",
        )}
      >
        {value}
      </div>
    </div>
  );
}

function MetricTile({
  icon: Icon,
  label,
  value,
}: {
  readonly icon: LucideIcon;
  readonly label: string;
  readonly value: string;
}) {
  return (
    <div className="min-w-0 rounded-md bg-white/76 px-3 py-3 shadow-[inset_0_0_0_1px_rgba(20,24,31,0.06)]">
      <Icon className="mb-2 size-4 text-stone-500" aria-hidden="true" />
      <div className="truncate text-base font-semibold leading-5 tabular-nums text-stone-950">
        {value}
      </div>
      <div className="mt-1 text-[0.68rem] font-medium leading-4 text-stone-500">{label}</div>
    </div>
  );
}

function FeaturePillar({
  copy,
  icon: Icon,
  title,
}: {
  readonly copy: string;
  readonly icon: LucideIcon;
  readonly title: string;
}) {
  return (
    <article className="rounded-lg border border-white/70 bg-white/58 p-5 shadow-[inset_0_1px_0_rgba(255,255,255,0.82),0_20px_54px_-42px_rgba(20,24,31,0.55)] backdrop-blur">
      <div className="mb-5 grid size-9 place-items-center rounded-md bg-stone-950 text-white shadow-[0_14px_30px_-20px_rgba(20,24,31,0.8)]">
        <Icon className="size-4" aria-hidden="true" />
      </div>
      <h2 className="text-base font-semibold text-stone-950">{title}</h2>
      <p className="mt-2 text-sm leading-6 text-stone-600">{copy}</p>
    </article>
  );
}
