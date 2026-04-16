"use client";

import * as React from "react";

import { cn } from "@forge-metal/ui/lib/utils";
import { useActiveAnchor } from "@forge-metal/ui/hooks/use-active-anchor";

export type AnchorSection = {
  readonly id: string;
  readonly label: string;
};

export type AnchorNavChipsProps = {
  readonly sections: readonly AnchorSection[];
  readonly label?: string;
  readonly className?: string;
};

// AnchorNavChips — mobile-only sticky horizontal chip strip that pairs
// with a nested left-rail on desktop. Each chip is an <a href="#id"> so
// native hash navigation + smooth-scroll both work; the active chip is
// set by scroll-spy and auto-scrolled into view so it never falls off
// screen on long pages.
function AnchorNavChips({ sections, label = "On this page", className }: AnchorNavChipsProps) {
  const active = useActiveAnchor(sections);
  const railRef = React.useRef<HTMLElement>(null);

  React.useEffect(() => {
    if (!active || !railRef.current) return;
    const el = railRef.current.querySelector<HTMLAnchorElement>(
      `a[href="#${CSS.escape(active)}"]`,
    );
    el?.scrollIntoView({ behavior: "smooth", block: "nearest", inline: "center" });
  }, [active]);

  if (sections.length === 0) return null;

  return (
    <nav
      ref={railRef}
      aria-label={label}
      data-slot="anchor-nav-chips"
      className={cn(
        "sticky top-14 z-20 -mx-4 flex gap-1 overflow-x-auto border-b border-border bg-background/95 px-4 py-2 backdrop-blur md:hidden",
        className,
      )}
    >
      {sections.map((section) => (
        <a
          key={section.id}
          href={`#${section.id}`}
          aria-current={section.id === active ? "location" : undefined}
          data-active={section.id === active ? "true" : "false"}
          className={cn(
            "whitespace-nowrap rounded-md px-3 py-1.5 text-sm transition-colors",
            "data-[active=false]:text-muted-foreground data-[active=false]:hover:bg-accent data-[active=false]:hover:text-foreground",
            "data-[active=true]:bg-accent data-[active=true]:font-medium data-[active=true]:text-foreground",
          )}
        >
          {section.label}
        </a>
      ))}
    </nav>
  );
}

export { AnchorNavChips };
