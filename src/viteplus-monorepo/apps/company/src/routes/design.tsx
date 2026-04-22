import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect, useMemo, useRef } from "react";
import { useActiveAnchor } from "@forge-metal/ui/hooks/use-active-anchor";
import { cn } from "@forge-metal/ui/lib/utils";
import { DESIGN_GROUPS, DESIGN_SECTIONS, type DesignSection } from "~/lib/design-nav";
import { emitSpan } from "~/lib/telemetry/browser";
import { DesignSections } from "~/features/design/sections";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/design")({
  component: DesignPage,
  head: () => ({
    meta: ogMeta({
      slug: "design",
      title: "Guardian Intelligence — Brand System",
      description:
        "The Guardian Intelligence brand system: the mark, the lockups, the type, the colour, and how they appear in the surfaces the firm actually ships.",
    }),
    links: [{ rel: "canonical", href: "/design" }],
  }),
});

function DesignPage() {
  return (
    <div
      // /design is a single page with its own dark canvas. Scoping the iron
      // ground here means the rest of the platform app keeps its existing
      // light treatment without a global theme toggle.
      style={{
        background: "var(--color-iron)",
        color: "var(--color-type-iron)",
        minHeight: "100vh",
      }}
    >
      {/*
       * /design gets its own max-width envelope — wider than the rest of the
       * site's `max-w-7xl` (1280 px) so the per-treatment rules row can
       * actually deploy its two-column grammar at very wide viewports
       * (≥ 1520 px). The company-site marketing routes stay at 7xl; this
       * expanded container is scoped to the brand-system page because the
       * mark specimen + type ladder pair is the only place we benefit from
       * the extra horizontal real estate.
       */}
      <div className="mx-auto w-full max-w-[96rem] px-4 py-10 md:px-6 md:py-14">
        <div className="flex flex-col gap-6 md:flex-row md:items-start md:gap-12">
          <DesignRail />
          <div className="min-w-0 flex-1">
            <DesignHeader />
            <DesignSections />
          </div>
        </div>
      </div>
    </div>
  );
}

function DesignHeader() {
  return (
    <header className="mb-16 flex flex-col gap-4">
      <p
        className="font-mono text-[11px] font-semibold uppercase tracking-[0.16em]"
        style={{ color: "var(--muted)", fontVariationSettings: '"wght" 600' }}
      >
        Guardian Intelligence · Brand System
      </p>
      <h1
        className="font-display text-4xl leading-tight md:text-6xl"
        style={{
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          letterSpacing: "-0.028em",
          fontWeight: 400,
          margin: 0,
        }}
      >
        Applied — the brand, in operation.
      </h1>
      <p className="max-w-3xl text-base leading-relaxed" style={{ color: "var(--muted-strong)" }}>
        The wings are always <b>Argent</b> (#FFFFFF); three SIL-Open-Font-Licensed families —
        Fraunces for voice, Geist for work, Geist Mono for the machine — carry the type. Every
        section below is one of four treatments the firm ships in — <b>Company</b>, <b>Workshop</b>,{" "}
        <b>Newsroom</b>, <b>Letters</b> — each declaring its own palette, typography, and mark
        usage, plus the two cross-treatment rules in <i>Applied</i>.
      </p>
    </header>
  );
}

function DesignRail() {
  const sections = useMemo(() => DESIGN_SECTIONS.map((s) => ({ id: s.id })), []);
  const active = useActiveAnchor(sections);
  const lastEmitted = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!active || active === lastEmitted.current) return;
    const meta = DESIGN_SECTIONS.find((s) => s.id === active);
    if (!meta) return;
    lastEmitted.current = active;
    emitSpan("design.section_view", {
      "section.id": meta.id,
      "section.number": meta.number,
      "section.group": meta.group,
    });
  }, [active]);

  const groups = DESIGN_GROUPS.map((group) => (
    <DesignRailGroup
      key={group}
      group={group}
      sections={DESIGN_SECTIONS.filter((s) => s.group === group)}
      active={active}
    />
  ));

  return (
    <nav
      aria-label="Design system sections"
      className="md:sticky md:top-[var(--header-scroll-offset)] md:w-60 md:shrink-0"
    >
      {/* On <md the rail swallows the whole first screen if rendered flat.
          Collapse into a <details> disclosure (closed by default) so mobile
          readers see the header + hero immediately and can opt into the rail.
          The desktop rail is a separate flat render — simpler than forcing
          open state via CSS at the md: breakpoint. */}
      <details className="md:hidden">
        <summary
          className="mb-4 flex cursor-pointer select-none items-center justify-between rounded-md px-3 py-2"
          style={{
            border: "1px solid rgba(245,245,245,0.12)",
            background: "rgba(245,245,245,0.04)",
          }}
        >
          <span
            className="font-mono text-[11px] font-semibold uppercase tracking-[0.18em]"
            style={{ color: "var(--muted)", fontVariationSettings: '"wght" 600' }}
          >
            Jump to section
          </span>
          <span aria-hidden style={{ color: "var(--muted-faint)" }}>
            ▾
          </span>
        </summary>
        <div className="flex flex-col gap-1">{groups}</div>
      </details>
      <div className="hidden flex-col gap-1 md:flex">{groups}</div>
    </nav>
  );
}

function DesignRailGroup({
  group,
  sections,
  active,
}: {
  group: DesignSection["group"];
  sections: readonly DesignSection[];
  active: string | undefined;
}) {
  return (
    <div className="mb-4 flex flex-col">
      <p
        className="mb-1 px-2 font-mono text-[10px] font-semibold uppercase tracking-[0.18em]"
        style={{ color: "var(--muted)", fontVariationSettings: '"wght" 600' }}
      >
        {group}
      </p>
      <ul className="flex flex-col gap-0.5">
        {sections.map((section) => (
          <li key={section.id}>
            <DesignRailLink section={section} isActive={section.id === active} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function DesignRailLink({ section, isActive }: { section: DesignSection; isActive: boolean }) {
  return (
    <Link
      to="/design"
      hash={section.id}
      data-active={isActive ? "true" : "false"}
      data-testid={`design-nav-${section.id}`}
      onClick={() => {
        emitSpan("design.anchor_click", {
          "section.id": section.id,
          "section.number": section.number,
          "section.group": section.group,
        });
      }}
      className={cn(
        "group flex items-center gap-2 whitespace-nowrap rounded-md px-2 py-1.5 text-sm transition-colors",
        "data-[active=false]:hover:bg-white/5",
      )}
      style={{
        color: isActive ? "rgba(245,245,245,0.95)" : "var(--muted-faint)",
        fontWeight: isActive ? 500 : 400,
      }}
    >
      <span
        className="font-mono text-[10px] font-semibold"
        style={{
          // Active number sets in Argent, not Flare. Flare is the Company
          // accent — burning it on chrome muddies the "Flare only on Company
          // ground" rule the page declares two scrolls below.
          color: isActive ? "var(--color-argent)" : "var(--muted-meta)",
          fontVariationSettings: '"wght" 600',
        }}
      >
        {section.number}
      </span>
      <span>{section.label}</span>
    </Link>
  );
}
