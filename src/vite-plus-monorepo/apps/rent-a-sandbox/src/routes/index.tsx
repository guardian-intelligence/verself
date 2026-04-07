import { createFileRoute } from "@tanstack/react-router";
import { FeaturePanel, baselineCapabilities, summarizeCapabilities } from "@forge-metal/ui";

const verificationCommands = [
  "vp check",
  "vp test run",
  "vp run -r typecheck",
  "vp run -r build",
] as const;

export const Route = createFileRoute("/")({
  component: Home,
});

function Home() {
  return (
    <div className="space-y-10">
      <section className="grid gap-6 lg:grid-cols-[minmax(0,1.35fr)_minmax(20rem,0.9fr)]">
        <div className="rounded-[2rem] border border-white/10 bg-white/5 p-8 shadow-[0_30px_80px_rgba(2,6,23,0.45)] backdrop-blur">
          <p className="text-sm font-medium uppercase tracking-[0.3em] text-amber-200/70">
            Rent-a-Sandbox
          </p>
          <h1 className="mt-5 max-w-3xl text-4xl font-semibold tracking-tight text-white sm:text-5xl">
            The Vite+ landing zone is ready for the frontend cutover.
          </h1>
          <p className="mt-5 max-w-2xl text-base leading-7 text-slate-300 sm:text-lg">
            This repo now has a dedicated pnpm workspace, a TanStack Start app wired for Nitro, and
            one shared package with real test coverage. The next change can focus on moving product
            code instead of building tooling from scratch.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Badge>pnpm workspace</Badge>
            <Badge>TanStack Start</Badge>
            <Badge>Tailwind 4</Badge>
            <Badge>Vite+ checks</Badge>
          </div>
        </div>

        <aside className="rounded-[2rem] border border-amber-400/25 bg-amber-500/10 p-8 shadow-[0_24px_60px_rgba(120,53,15,0.18)] backdrop-blur">
          <p className="text-sm font-medium uppercase tracking-[0.3em] text-amber-100/80">
            Verification
          </p>
          <p className="mt-4 text-2xl font-semibold text-white">
            {summarizeCapabilities(baselineCapabilities)}
          </p>
          <ul className="mt-6 space-y-3 text-sm text-amber-50/85">
            {verificationCommands.map((command) => (
              <li
                key={command}
                className="rounded-2xl border border-white/10 bg-slate-950/55 px-4 py-3 font-mono"
              >
                {command}
              </li>
            ))}
          </ul>
        </aside>
      </section>

      <section className="grid gap-4 lg:grid-cols-3">
        {baselineCapabilities.map((capability, index) => (
          <FeaturePanel
            key={capability.title}
            capability={capability}
            tone={index === 1 ? "accent" : "default"}
          />
        ))}
      </section>
    </div>
  );
}

function Badge({ children }: { children: string }) {
  return (
    <span className="rounded-full border border-white/10 bg-slate-900/80 px-3 py-1 text-sm text-slate-200">
      {children}
    </span>
  );
}
