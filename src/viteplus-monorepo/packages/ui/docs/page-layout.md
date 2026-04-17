# Page layout and visual rhythm

One opinionated page-layout contract for every Forge Metal app. Every
route in every app flows through the primitives in
`@forge-metal/ui/components/ui/page`, which own spacing and typography
so route files describe hierarchy and content only.

This document is the source of truth. If a screen disagrees with this
doc, the screen is wrong.

## Invariants

1. **Every route renders exactly one `<Page>` wrapper.** Never two.
2. **Every route has at most one `<PageHeader>` with exactly one `<PageTitle>`.**
3. **`<PageSection>` is bare by default.** Wrap in `<Card>` only when the
   content _is_ a distinct object with its own affordance (a plan hero, a
   metric tile, a credit pack). Lists of members, capabilities, statements,
   and tables use bare sections.
4. **Spacing is owned by the primitives.** A route must not set
   `space-y-*`, `gap-*`, `mt-*`, or `mb-*` on `<Page>`, `<PageSections>`,
   `<PageSection>`, `<PageHeader>`, or `<SectionHeader>`. Override the
   rhythm and the next page drifts — do not do it.
5. **Typography is owned by the primitives.** `PageTitle`, `SectionTitle`,
   `PageDescription`, and `SectionDescription` carry the full text style.
   A route must not set `text-*`, `font-*`, or `tracking-*` on those
   elements.
6. **Width is owned by the shell and the `variant` prop.** The app shell
   provides the outer 1152px column. Routes pick one of three variants and
   do not set `max-w-*` anywhere else.

## The primitives

```tsx
import {
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
} from "@forge-metal/ui/components/ui/page";
```

### Composition tree

```
Page (variant?)                         ← root wrapper; gap-10 between header and sections
├─ PageHeader                           ← flex row, gap-6 horizontal
│  ├─ PageHeaderContent                 ← left side; gap-1 between title, description, eyebrow
│  │  ├─ PageEyebrow?                   ← optional breadcrumb / back link above the title
│  │  ├─ PageTitle                      ← single h1
│  │  └─ PageDescription?
│  └─ PageActions?                      ← right side CTAs
└─ PageSections                         ← gap-8 between each child PageSection
   └─ PageSection                       ← gap-4 between its SectionHeader and body
      ├─ SectionHeader?                 ← optional
      │  ├─ SectionHeaderContent
      │  │  ├─ SectionTitle             ← h2
      │  │  └─ SectionDescription?
      │  └─ SectionActions?
      └─ (body — lists, tables, forms, grids)
```

### Variants

The `Page` component takes a `variant` prop that sets the page's max
width. It is the **only** place a route decides width.

| `variant` | max width                      | When to use                                      |
| --------- | ------------------------------ | ------------------------------------------------ |
| `default` | 1152px inherited from AppShell | Dashboards, list routes, detail routes           |
| `narrow`  | `max-w-2xl` (672px)            | Forms, short-form flows, single-focus pages      |
| `full`    | `max-w-none`                   | Data tables or canvas views that need to breathe |

## The spacing scale

One scale. Four rhythm tokens. Baked into primitives, never overridden by
routes.

| Token                  | Tailwind | Pixels | Where it lives                                               |
| ---------------------- | -------- | ------ | ------------------------------------------------------------ |
| header-to-body         | `gap-10` | 40px   | `<Page>` — between PageHeader and PageSections               |
| section-to-section     | `gap-8`  | 32px   | `<PageSections>` — between adjacent PageSections             |
| section-header-to-body | `gap-4`  | 16px   | `<PageSection>` — between SectionHeader and the section body |
| intra-section-group    | `gap-6`  | 24px   | form rows, card grids, subgroup rhythm inside a section      |

**Why 40/32/16.** Distinguished dashboards (Linear, Vercel, Radix Themes)
converge on "generous between landmarks, tight between group members."
40px between the PageHeader and the first section establishes the
PageHeader as a clear landmark; 32px between sections is enough to read
as separated without blowing the page below the fold; 16px between a
section title and its body reads as "title → body," not "title, blank,
body."

## The type scale

One scale. Five roles. Callers pick a **role**, not a size.

| Role                | Component            | Tailwind                                    | px  | Weight |
| ------------------- | -------------------- | ------------------------------------------- | --- | ------ |
| Page title          | `PageTitle`          | `text-2xl font-semibold tracking-tight`     | 24  | 600    |
| Page description    | `PageDescription`    | `text-sm text-muted-foreground`             | 14  | 400    |
| Page eyebrow        | `PageEyebrow`        | `text-xs font-medium text-muted-foreground` | 12  | 500    |
| Section title       | `SectionTitle`       | `text-sm font-semibold`                     | 14  | 600    |
| Section description | `SectionDescription` | `text-xs text-muted-foreground`             | 12  | 400    |

**Why section titles are 14px / semibold** (deliberately eyebrow-weight,
not large). Once the PageHeader is a clear 24px landmark and the
section-to-section rhythm is 32px, a 20px section title competes with
the page title and muddies the hierarchy. Linear, Stripe Dashboard, and
Vercel's project console all use eyebrow-weight section titles for this
reason. Radix Themes uses a slightly larger 16px — we're one step
tighter because our grid is dense.

### Body, numeric, and code

For the body of a section (table cells, form fields, descriptive text),
the default is `text-sm` (14px). No primitive enforces this because
shadcn's own primitives (`Table`, `Input`, `Label`, `Field`) already
land at 14px. Override only when you have a reason, and write the
reason down.

Numeric data that scans vertically — account balances, metric values,
statement rows, ID prefixes — must use `font-mono tabular-nums` so that
digits align across rows and across renders.

## Card vs bare section

`PageSection` is bare by default. Wrap content in `<Card>` only when
**the content is an object** — something with its own shadow,
affordance, and identity. Not when it is "a section of a page."

```tsx
// ✅ Object (distinct affordance, hero treatment)
<PageSection>
  <Card>
    <CardHeader>
      <CardTitle>Pro plan</CardTitle>
    </CardHeader>
    <CardContent>...</CardContent>
  </Card>
</PageSection>

// ✅ Section (bare — title is a landmark, not an object)
<PageSection>
  <SectionHeader>
    <SectionHeaderContent>
      <SectionTitle>Usage</SectionTitle>
      <SectionDescription>Current cycle started …</SectionDescription>
    </SectionHeaderContent>
  </SectionHeader>
  <table>…</table>
</PageSection>

// ❌ Section wrapped in Card for no reason — adds visual noise,
// duplicates the rhythm the Page primitive already enforces
<PageSection>
  <Card>
    <CardHeader>
      <CardTitle>Members</CardTitle>
    </CardHeader>
    <CardContent>
      <table>…</table>
    </CardContent>
  </Card>
</PageSection>
```

Concrete examples from the current codebase:

- **PlanHero** in `settings/billing/index.tsx` — Card-like, bordered,
  has its own shadow. It's the object "the plan you're on." Renders
  with a border.
- **Credit packs** in `settings/billing/credits.tsx` — each `$10 / $25
/ …` tile is an object. `Card`.
- **Plan tiles** in `settings/billing/subscribe.tsx` — each plan is an
  object you can click. `Card`.
- **Usage statement** in `settings/billing/index.tsx` — a section of
  the billing page, not an object. Bare `PageSection` + bordered table.
- **Members list** in the organization profile — a section. Bare.
- **Member capabilities** — a section. Bare, though the switch list
  inside carries its own bordered group because the switches are a
  compound control.

## Page anatomy examples

### Simple list page

```tsx
function ExecutionsPage() {
  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Executions</PageTitle>
          <PageDescription>Direct VM executions, billing windows, and logs.</PageDescription>
        </PageHeaderContent>
        <PageActions>
          <Button>New execution</Button>
        </PageActions>
      </PageHeader>
      <PageSections>
        <PageSection>
          <ExecutionTable />
        </PageSection>
      </PageSections>
    </Page>
  );
}
```

### Detail page with eyebrow and section titles

```tsx
function ExecutionDetailPage() {
  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageEyebrow>
            <Link to="/executions">← Executions</Link>
          </PageEyebrow>
          <div className="flex items-center gap-3">
            <PageTitle className="font-mono">c9df6c3e</PageTitle>
            <StatusBadge status="succeeded" />
          </div>
        </PageHeaderContent>
      </PageHeader>
      <PageSections>
        <PageSection>
          <ExecutionTimingSummary />
        </PageSection>
        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Output</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <LogsBody />
        </PageSection>
      </PageSections>
    </Page>
  );
}
```

### Narrow form page

```tsx
function NewExecutionPage() {
  return (
    <Page variant="narrow">
      <PageHeader>
        <PageHeaderContent>
          <PageEyebrow>
            <Link to="/executions">← Executions</Link>
          </PageEyebrow>
          <PageTitle>New execution</PageTitle>
          <PageDescription>Direct executions run in a fresh VM.</PageDescription>
        </PageHeaderContent>
      </PageHeader>
      <PageSections>
        <PageSection>
          <form>…</form>
        </PageSection>
      </PageSections>
    </Page>
  );
}
```

### Sub-route that reuses a parent's PageHeader

When a sub-route renders inside a layout that already owns the
`<Page>` + `<PageHeader>` (the settings layout is the main example),
the child route must NOT render its own `Page` or `PageHeader`. It
renders `<PageSections>` directly:

```tsx
function BillingPage() {
  return (
    <PageSections>
      <PageSection>…</PageSection>
      <PageSection>…</PageSection>
    </PageSections>
  );
}
```

Sub-sub-routes that are conceptually their own "page" but still live
inside the parent layout (e.g. `/settings/billing/credits`, which you
reach from a button inside the billing page) use a `PageEyebrow` for
the back-link and a `SectionHeader` with `SectionTitle` for the sub-page
title — not a second `PageTitle`, because there is still only one `h1`
on the page.

```tsx
function CreditsPage() {
  return (
    <PageSections>
      <PageSection>
        <PageEyebrow>
          <Link to="/settings/billing">← Back to billing</Link>
        </PageEyebrow>
        <SectionHeader>
          <SectionHeaderContent>
            <SectionTitle>Purchase credits</SectionTitle>
            <SectionDescription>Add prepaid account balance.</SectionDescription>
          </SectionHeaderContent>
        </SectionHeader>
        <CreditPackGrid />
      </PageSection>
    </PageSections>
  );
}
```

## Color tokens

Page layout primitives only use three color tokens. All three are
Tailwind v4 `@theme` `--color-*` tokens defined in each app's
`app.css`:

- `bg-background` / `text-foreground` — page canvas
- `text-muted-foreground` — descriptions, eyebrows, labels
- `border` / `border-border` / `var(--color-border)` — any outline

Everything else (accent colors, semantic success/warning/danger, card
surfaces) belongs to the content, not the layout. The layout is
deliberately monochromatic so that when we introduce color, the color
is doing semantic work.

### Which token set we actually use

Stock shadcn v3 shipped legacy CSS variables like `--sidebar-border`,
`--sidebar-accent`, etc., referenced in class strings as
`hsl(var(--sidebar-border))`. **Forge Metal apps do not import that
token set.** We only define the Tailwind v4 `--color-*` tokens inside
each app's `app.css` `@theme` block. If you write (or borrow) a
shadcn class that references `--sidebar-*`, `--popover-*`, etc., the
variable will resolve to an empty string and the style will silently
disappear — no error, no warning, no visible effect.

Translation table when porting from upstream shadcn examples:

| Upstream shadcn              | Our codebase          |
| ---------------------------- | --------------------- |
| `hsl(var(--sidebar-border))` | `var(--color-border)` |
| `hsl(var(--sidebar-accent))` | `var(--color-accent)` |
| `hsl(var(--border))`         | `var(--color-border)` |
| `bg-sidebar-accent`          | `bg-accent`           |

This was the root cause of a silent sidebar-outline bug: the
variant class shipped from upstream shadcn referenced
`hsl(var(--sidebar-border))`, which resolved to nothing, so the 1px
ring on the account row was invisible. Never reach for
`--sidebar-*` variables in new code.

## Checklist for a new route

Before merging a new page, confirm:

- [ ] Exactly one `<Page>` wrapper.
- [ ] Exactly one `<PageTitle>` (or none, if the parent layout owns it).
- [ ] No `space-y-*` / `gap-*` / `mt-*` / `mb-*` on `Page`, `PageSections`,
      `PageSection`, `PageHeader`, or `SectionHeader`.
- [ ] No `text-*` or `font-*` on `PageTitle`, `SectionTitle`,
      `PageDescription`, or `SectionDescription` (with one exception:
      `font-mono` is allowed on titles that are machine identifiers).
- [ ] No `max-w-*` anywhere in the route file — use `<Page variant>` instead.
- [ ] Sections that aren't distinct objects are **bare** (no `Card`).
- [ ] Numeric columns in tables use `font-mono tabular-nums`.
- [ ] Empty / loading / error states render inside a `PageSection` body
      (not a top-level `<div>` outside the rhythm).

## Migrating existing Tailwind

If you encounter a route that still hand-rolls its layout
(`<div className="space-y-6"><header>…</header>…</div>`), the migration
is mechanical:

1. Replace the root `<div>` with `<Page>` (or `<Page variant="narrow">`
   for forms).
2. Replace the header `<div>`/`<header>` with `<PageHeader><PageHeaderContent>…</PageHeaderContent></PageHeader>`.
3. Replace `<h1 className="text-2xl font-semibold tracking-tight">` with `<PageTitle>`.
4. Replace `<p className="text-sm text-muted-foreground">` with `<PageDescription>`.
5. Wrap the sibling content in `<PageSections><PageSection>…</PageSection></PageSections>`.
6. Drop any `space-y-6` / `space-y-8` on the outer container — the
   primitives own that now.
7. For each `h2 className="text-sm font-semibold"` section label inside
   the body, replace with `<SectionHeader><SectionHeaderContent><SectionTitle>…</SectionTitle></SectionHeaderContent></SectionHeader>`.

## Non-goals

This primitive set does **not**:

- Ship a theme builder, a token-generation pipeline, or runtime
  customization. The tokens are Tailwind classes, hardcoded.
- Replace `Card`, `Button`, `Table`, or any other shadcn primitive.
  They continue to be the contents of pages.
- Enforce dark mode, color schemes, or any semantic color. That is the
  existing `globals.css` job.
- Handle the app shell itself (sidebar, top bar, command palette).
  Those live in each app's shell module.

If you want any of the above, propose it separately.
