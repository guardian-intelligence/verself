import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect, useMemo, useRef } from "react";
import { useActiveAnchor } from "@forge-metal/ui/hooks/use-active-anchor";
import { cn } from "@forge-metal/ui/lib/utils";
import { DESIGN_GROUPS, DESIGN_SECTIONS, type DesignSection } from "~/lib/design-nav";
import { emitSpan } from "~/lib/telemetry/browser";
import { DesignSections } from "~/features/design/sections";

export const Route = createFileRoute("/design")({
  component: DesignPage,
  head: () => ({
    meta: [
      { title: "Guardian Intelligence — Brand System" },
      {
        name: "description",
        content:
          "The Guardian Intelligence brand system: the mark, the lockups, the type, the colour, and how they appear in the surfaces the company actually ships.",
      },
      { name: "theme-color", content: "#0E0E0E" },
      { property: "og:type", content: "website" },
      { property: "og:title", content: "Guardian Intelligence — Brand System" },
      {
        property: "og:description",
        content:
          "Two wings. Three grounds. Three families. The brand system that runs from favicon to signage.",
      },
      { property: "og:image", content: "/og/design" },
      { property: "og:image:type", content: "image/svg+xml" },
      { property: "og:image:width", content: "1200" },
      { property: "og:image:height", content: "630" },
      { name: "twitter:card", content: "summary_large_image" },
      { name: "twitter:image", content: "/og/design" },
    ],
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
      <div className="mx-auto w-full max-w-7xl px-4 py-10 md:px-6 md:py-14">
        <div className="flex flex-col gap-10 md:flex-row md:items-start md:gap-12">
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
        className="font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
        style={{ color: "rgba(245,245,245,0.55)" }}
      >
        Guardian Intelligence · Brand System v0.1
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
      <p
        className="max-w-3xl text-base leading-relaxed"
        style={{ color: "rgba(245,245,245,0.72)" }}
      >
        Guardian Intelligence is an American applied intelligence company, based in Seattle. Fraunces
        carries the voice. Geist carries the work. Geist Mono carries the machine — three families,
        all under the SIL Open Font License, no commercial fee, ever. The system runs on three
        grounds — <b>Iron</b>, <b>Flare</b>, and <b>Paper</b> — one invariant —{" "}
        <b>the wings are always Argent</b> — and two carrier forms that let the wings travel: an
        iron chip on Paper, and a circular ink emboss on Flare. Only Guardian carries the wings;
        products — <b>Metal</b>, <b>Console</b> — inherit the house and set their own name in
        Geist. This page specifies the mark, the type, the colour, and how they appear in the
        surfaces the company actually ships: marketing, editorial, product, and external.
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

  return (
    <nav
      aria-label="Design system sections"
      className="md:sticky md:top-[var(--header-scroll-offset)] md:w-60 md:shrink-0"
    >
      <div className="flex flex-col gap-1">
        {DESIGN_GROUPS.map((group) => (
          <DesignRailGroup
            key={group}
            group={group}
            sections={DESIGN_SECTIONS.filter((s) => s.group === group)}
            active={active}
          />
        ))}
      </div>
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
        className="mb-1 px-2 font-mono text-[10px] font-medium uppercase tracking-[0.18em]"
        style={{ color: "rgba(245,245,245,0.4)" }}
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
        color: isActive ? "rgba(245,245,245,0.95)" : "rgba(245,245,245,0.55)",
        fontWeight: isActive ? 500 : 400,
      }}
    >
      <span
        className="font-mono text-[10px]"
        style={{ color: isActive ? "var(--color-flare)" : "rgba(245,245,245,0.35)" }}
      >
        {section.number}
      </span>
      <span>{section.label}</span>
    </Link>
  );
}
