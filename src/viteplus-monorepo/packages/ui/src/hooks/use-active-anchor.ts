"use client";

import * as React from "react";

// useActiveAnchor — scroll-spy for anchor-style navigation. Given a list
// of `{ id }` entries matching DOM element IDs on the current page, it
// returns the ID of the section currently in view. Deep-linked hashes
// prime the initial state so a freshly loaded `/docs/reference#schemas`
// highlights the right entry before the first observer tick.
//
// The rootMargin used by the IntersectionObserver is derived from the
// `--header-scroll-offset` CSS custom property when present, so the
// "active" band always matches the `scroll-padding-top` the browser
// uses for hash jumps. Falls back to 80px when the var is unset.
//
// SSR-safe: the observer wires up on mount, so the first render has no
// active state and won't mismatch between server and client.

export type ActiveAnchorEntry = { readonly id: string };

const FALLBACK_OFFSET_PX = 80;

function resolveCssLength(value: string): number | undefined {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  // Custom property values are returned in their authored form ("3.5rem",
  // "calc(...)", etc.) and aren't resolved to px. A one-off hidden probe
  // element lets the browser do the math for us without a special case
  // for every unit.
  const probe = document.createElement("div");
  probe.style.position = "absolute";
  probe.style.visibility = "hidden";
  probe.style.pointerEvents = "none";
  probe.style.height = trimmed;
  document.body.appendChild(probe);
  const px = probe.getBoundingClientRect().height;
  document.body.removeChild(probe);
  return Number.isFinite(px) ? px : undefined;
}

function resolveHeaderOffset(): number {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(
    "--header-scroll-offset",
  );
  return resolveCssLength(raw) ?? FALLBACK_OFFSET_PX;
}

export function useActiveAnchor(entries: readonly ActiveAnchorEntry[]): string | undefined {
  const [active, setActive] = React.useState<string | undefined>(undefined);

  React.useEffect(() => {
    if (entries.length === 0) return;
    if (typeof IntersectionObserver === "undefined") return;

    const hash = window.location.hash.slice(1);
    if (hash && entries.some((e) => e.id === hash)) setActive(hash);

    const topOffset = resolveHeaderOffset();
    const visible = new Map<string, number>();
    const observer = new IntersectionObserver(
      (records) => {
        for (const record of records) {
          if (record.isIntersecting) visible.set(record.target.id, record.intersectionRatio);
          else visible.delete(record.target.id);
        }
        // First-in-document-order visible section wins, so the highlight
        // tracks downward with scroll instead of jumping by ratio.
        const first = entries.find((e) => visible.has(e.id));
        if (first) setActive(first.id);
      },
      {
        // Active band lives just below the sticky header. The bottom of
        // the band (-60%) pushes the active state down until a heading
        // sits comfortably within the visible page area, not just as its
        // top edge peeks in.
        rootMargin: `-${Math.round(topOffset)}px 0px -60% 0px`,
        threshold: [0, 1],
      },
    );

    for (const entry of entries) {
      const el = document.getElementById(entry.id);
      if (el) observer.observe(el);
    }
    return () => observer.disconnect();
  }, [entries]);

  return active;
}
