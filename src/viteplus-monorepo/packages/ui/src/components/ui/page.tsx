import * as React from "react";

import { cn } from "@forge-metal/ui/lib/utils";

// Page layout primitives for Forge Metal apps. These enforce one
// opinionated rhythm, one typography scale, and one container-width
// decision across every route in every app. Callers describe the page's
// *information hierarchy* (header, sections, titles) and never pick
// spacing or font-size themselves. See packages/ui/docs/page-layout.md
// for the rationale and rules.
//
// Typography scale — three sizes, three weights, two greys:
//   Display  28/34 · 600 · foreground           → PageTitle (h1)
//   Heading  18/26 · 600 · foreground           → SectionTitle (h2)
//   Body     14/20 · 400 · foreground           → default prose
//   Caption  12/16 · 500 · muted-foreground     → PageDescription, SectionDescription, labels
//
// Rhythm:
//   Page          → gap-10 (40px) between header and PageSections
//   PageSections  → gap-12 (48px) between sibling sections (loose, so sections don't blur together)
//   PageSection   → gap-3  (12px) between section header and its body (tight, so the header groups with its content)
//
// Quick reference:
//   Page                 — root wrapper. variant="default" is the 1152px column
//                          inherited from the AppShell; "narrow" clamps to a
//                          672px form column; "full" removes the clamp (data
//                          tables that need to breathe).
//   PageHeader           — title + description on the left, actions on the right.
//   PageTitle            — the page's h1. Display size (28px semibold).
//   PageDescription      — muted caption beneath the title.
//   PageActions          — trailing action buttons, right-aligned.
//   PageSection          — semantic <section>. Bare by default; wrap in Card only
//                          when the content IS an object (hero/tile), not a section.
//   SectionHeader        — row carrying SectionTitle + SectionDescription +
//                          SectionActions, with canonical gap-3 to its body.
//   SectionTitle         — the section's h2. Heading size (18px semibold).
//   SectionDescription   — muted caption beneath the section title.
//   SectionActions       — trailing action slot for a section.

type PageVariant = "default" | "narrow" | "full";

const PAGE_VARIANT_CLASS: Record<PageVariant, string> = {
  default: "",
  narrow: "max-w-2xl",
  full: "max-w-none",
};

function Page({
  className,
  variant = "default",
  ...props
}: React.ComponentProps<"div"> & { variant?: PageVariant }) {
  return (
    <div
      data-slot="page"
      data-variant={variant}
      className={cn("flex flex-col gap-10", PAGE_VARIANT_CLASS[variant], className)}
      {...props}
    />
  );
}

function PageHeader({ className, ...props }: React.ComponentProps<"header">) {
  return (
    <header
      data-slot="page-header"
      className={cn("flex flex-wrap items-start justify-between gap-x-6 gap-y-4", className)}
      {...props}
    />
  );
}

function PageHeaderContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="page-header-content"
      className={cn("flex min-w-0 flex-col gap-1", className)}
      {...props}
    />
  );
}

function PageEyebrow({ className, ...props }: React.ComponentProps<"nav">) {
  // Small breadcrumb or back-link row that renders above the PageTitle.
  return (
    <nav
      aria-label="Breadcrumb"
      data-slot="page-eyebrow"
      className={cn("flex items-center gap-2 text-xs font-medium text-muted-foreground", className)}
      {...props}
    />
  );
}

function PageTitle({ className, ...props }: React.ComponentProps<"h1">) {
  return (
    <h1
      data-slot="page-title"
      className={cn("text-[1.75rem] font-semibold leading-9 tracking-tight", className)}
      {...props}
    />
  );
}

function PageDescription({ className, ...props }: React.ComponentProps<"p">) {
  return (
    <p
      data-slot="page-description"
      className={cn("text-xs font-medium text-muted-foreground", className)}
      {...props}
    />
  );
}

function PageActions({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="page-actions"
      className={cn("flex shrink-0 flex-wrap items-center gap-2", className)}
      {...props}
    />
  );
}

function PageSections({ className, ...props }: React.ComponentProps<"div">) {
  // Container for multiple PageSections. Loose inter-section gap (48px) so
  // sibling sections read as *separate*; callers never pick space-y-*.
  return (
    <div data-slot="page-sections" className={cn("flex flex-col gap-12", className)} {...props} />
  );
}

function PageSection({ className, ...props }: React.ComponentProps<"section">) {
  // Tight section rhythm: 12px between the SectionHeader and its body so the
  // header visually groups with its content rather than orphaning.
  return (
    <section data-slot="page-section" className={cn("flex flex-col gap-3", className)} {...props} />
  );
}

function SectionHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="section-header"
      className={cn("flex flex-wrap items-start justify-between gap-x-6 gap-y-2", className)}
      {...props}
    />
  );
}

function SectionHeaderContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="section-header-content"
      className={cn("flex min-w-0 flex-col gap-1", className)}
      {...props}
    />
  );
}

function SectionTitle({ className, ...props }: React.ComponentProps<"h2">) {
  return (
    <h2
      data-slot="section-title"
      className={cn("text-lg font-semibold leading-6 tracking-tight", className)}
      {...props}
    />
  );
}

function SectionDescription({ className, ...props }: React.ComponentProps<"p">) {
  return (
    <p
      data-slot="section-description"
      className={cn("text-xs font-medium text-muted-foreground", className)}
      {...props}
    />
  );
}

function SectionActions({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="section-actions"
      className={cn("flex shrink-0 flex-wrap items-center gap-2", className)}
      {...props}
    />
  );
}

export {
  Page,
  PageHeader,
  PageHeaderContent,
  PageEyebrow,
  PageTitle,
  PageDescription,
  PageActions,
  PageSections,
  PageSection,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
  SectionDescription,
  SectionActions,
};
