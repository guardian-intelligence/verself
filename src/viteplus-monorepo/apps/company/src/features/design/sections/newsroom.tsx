import type { ReactNode } from "react";
import { NewsroomCard } from "../newsroom-card";
import { Section } from "../section-shell";
import { BizCard, sectionMeta } from "../shared";

// ============================================================================
// 02 — Treatments · Newsroom
//
// Newsroom is an Argent reading page carrying NewsroomCards: each card has a
// Flare top stripe (where the wordmark + logo always live) and an Argent
// body (kicker, Fraunces title, Flare CTA). The reference composition is
// Ramp's blog/news page: a calm body, a single featured card, a "Latest"
// grid underneath. Flare arrives in bounded moments — never as the room.
//
// The spec page IS the example: it renders a working Newsroom bulletin
// surface at the top, then trails specimen blocks (Controls card, Flare
// business card, spec list) that pin the load-bearing rules.
// ============================================================================
export function SectionNewsroom() {
  const meta = sectionMeta("newsroom");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Ultra simple: Argent body, NewsroomCards with a Flare top stripe for the logo and
          wordmark, Argent body for reading, Flare CTAs. Flare appears three times per card
          (stripe, CTA, stationery) — never as the room itself. Bordeaux never appears on
          Newsroom (Letters-only); Amber never appears on Newsroom (Workshop-only).
        </>
      }
    >
      {/* Working example — the Ramp-style bulletin page rendered inside a
          rounded frame so it reads as a specimen rather than as the page
          chrome. Synthetic "Sample" bulletins make the archive grid
          legible at real density. */}
      <SampleBulletinPage />

      <SectionDivider />

      {/* Controls specimen — the one full-Flare teaching block. A real
          Newsroom CTA sits inside so the rule ("CTAs on Newsroom are Flare
          on Ink text… wait, no — Flare background, Ink text") is
          demonstrated on the ground it will ship against. */}
      <ControlsCard />

      <SectionDivider />

      {/* Stationery specimen — a Flare business card for Newsroom contexts
          (press officers, events, broadcast communications). Iron chrome
          stays the default working card across the org; this is the
          Newsroom-scoped alternative. */}
      <StationeryBlock />

      <div style={{ marginTop: "24px" }}>
        <NewsroomSpecList
          rows={[
            { label: "Body ground", value: "#FFFFFF", note: "Argent — calm reading canvas" },
            {
              label: "Feature ground",
              value: "#CCFF00",
              note: "Pantone 389 C · Flare · card stripe + CTA + stationery",
            },
            { label: "Type", value: "#0B0B0B", note: "Ink on Argent and on Flare" },
            { label: "Mark", value: "WingsEmboss", note: "Argent wings in ink medallion, on Flare" },
            { label: "Hero title", value: "Fraunces", note: "opsz 144 · SOFT 30 · -0.026em" },
            { label: "Kicker", value: "Geist Mono", note: "11 / 1 / +180 · 600 · UPPER" },
            { label: "CTA", value: "Flare button · Ink text", note: "Rhymes with the card stripe" },
            { label: "Button radius", value: "rounded-md", note: "0.375rem · shadcn base" },
          ]}
          footer={
            <>
              Flare appears on three Newsroom surfaces per card: the top stripe carrying the
              logo + wordmark, the CTA button, and (where scoped) stationery. Body reading, kickers,
              bylines, and archive meta all set as Ink on Argent. If a page wants Flare in a fourth
              place, it probably wants Workshop (Amber) or Letters (Bordeaux) instead.
            </>
          }
        />
      </div>
    </Section>
  );
}

// ============================================================================
// SampleBulletinPage — a working Newsroom bulletin page, composed of one
// hero NewsroomCard + a three-up "Latest" grid of standard NewsroomCards.
// Sample content is clearly labeled so the reader does not mistake it for
// live bulletins.
// ============================================================================
function SampleBulletinPage() {
  return (
    <div
      style={{
        background: "var(--color-argent)",
        color: "var(--color-ink)",
        borderRadius: "20px",
        border: "1px solid rgba(11,11,11,0.12)",
        padding: "clamp(28px, 4vw, 48px)",
      }}
    >
      <ExampleBadge>Sample · Newsroom bulletin page</ExampleBadge>

      <div className="mt-6 flex flex-col gap-3">
        <h3
          style={{
            fontFamily: "'Geist', sans-serif",
            fontWeight: 500,
            fontSize: "clamp(24px, 2.8vw, 32px)",
            lineHeight: 1.1,
            letterSpacing: "-0.018em",
            color: "var(--color-ink)",
            margin: 0,
          }}
        >
          Newsroom
        </h3>
        <p
          className="max-w-2xl"
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "15px",
            lineHeight: 1.55,
            color: "rgba(11,11,11,0.6)",
            margin: 0,
          }}
        >
          Bulletins, milestones, and public notes from Guardian Intelligence.
        </p>
      </div>

      <div className="mt-8">
        <NewsroomCard
          size="hero"
          ariaLabel="Sample bulletin"
          kicker="Brand system · 19 April 2026"
          title="Three rooms, one house."
          cta={{ label: "See the rooms", href: "#sample" }}
        />
      </div>

      <p
        className="font-mono text-[11px] font-semibold uppercase tracking-[0.18em]"
        style={{
          color: "rgba(11,11,11,0.55)",
          fontVariationSettings: '"wght" 600',
          margin: "40px 0 14px",
        }}
      >
        Latest
      </p>
      <div
        className="grid gap-4 md:grid-cols-3"
        style={{ borderTop: "1px solid rgba(11,11,11,0.12)", paddingTop: "20px" }}
      >
        {SAMPLE_ARCHIVE.map((row) => (
          <NewsroomCard
            key={row.title}
            size="standard"
            kicker={`${row.kicker} · Sample`}
            title={row.title}
            blurb={row.blurb}
          />
        ))}
      </div>
    </div>
  );
}

type SampleArchiveRow = {
  readonly kicker: string;
  readonly title: string;
  readonly blurb: string;
};

const SAMPLE_ARCHIVE: readonly SampleArchiveRow[] = [
  {
    kicker: "Platform",
    title: "How a sandbox lease decides where it dies.",
    blurb:
      "A lease carries its own timers; the host never reasons about wall time. Specimen card — not a real bulletin.",
  },
  {
    kicker: "Milestones",
    title: "First customer runs a real workload.",
    blurb:
      "The Guardian execution path, end-to-end, with live billing. Specimen card — not a real bulletin.",
  },
  {
    kicker: "Guardian",
    title: "Why we pick the boring database every time.",
    blurb:
      "Postgres is load-bearing; excitement is technical debt. Specimen card — not a real bulletin.",
  },
];

// ============================================================================
// ControlsCard — teaches "CTAs on Newsroom are Flare background + Ink text."
// The block itself sits on Argent; the CTA inside IS the rule in living
// form. Intentionally pairs with a demonstration of the WRONG pattern
// (Ink-on-Flare) so a composer can see why the current rule holds.
// ============================================================================
function ControlsCard() {
  return (
    <div
      style={{
        padding: "clamp(28px, 4vw, 48px) clamp(20px, 4vw, 48px)",
        border: "1px solid rgba(11,11,11,0.12)",
        borderRadius: "16px",
        background: "var(--color-argent)",
        color: "var(--color-ink)",
      }}
    >
      <ExampleBadge>Specimen · Controls</ExampleBadge>
      <h3
        style={{
          fontFamily: "'Fraunces', Georgia, serif",
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: "clamp(28px, 3.6vw, 40px)",
          lineHeight: 1.05,
          letterSpacing: "-0.022em",
          color: "var(--color-ink)",
          margin: "16px 0 20px",
          maxWidth: "26ch",
        }}
      >
        CTAs on Newsroom are Flare, with Ink text.
      </h3>
      <p
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "15px",
          lineHeight: 1.55,
          color: "rgba(11,11,11,0.7)",
          margin: "0 0 24px",
          maxWidth: "56ch",
        }}
      >
        The primary control rhymes with the Flare stripe at the top of every NewsroomCard — one
        colour system, repeated at two scales. The button sits on the Argent body so Flare carries
        real contrast; the text sets in Ink with a{" "}
        <code style={{ fontFamily: "'Geist Mono', ui-monospace, monospace" }}>rounded-md</code>{" "}
        corner, matching the rest of the product.
      </p>
      <div className="flex flex-wrap items-center gap-4">
        <span
          className="rounded-md"
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: "8px",
            fontFamily: "'Geist', sans-serif",
            fontWeight: 500,
            fontSize: "14px",
            padding: "10px 18px",
            background: "var(--color-flare)",
            color: "var(--color-ink)",
            border: "none",
          }}
        >
          Read the letters
          <span aria-hidden="true">→</span>
        </span>
        <span
          className="font-mono text-[11px] font-semibold uppercase tracking-[0.18em]"
          style={{
            color: "rgba(11,11,11,0.55)",
            fontVariationSettings: '"wght" 600',
          }}
        >
          Flare · Ink · rounded-md
        </span>
      </div>
    </div>
  );
}

// ============================================================================
// StationeryBlock — a Flare business card for Newsroom-scoped stationery.
// Iron chrome remains the default working card across the org (see Applied
// footer); this one rides when Guardian is speaking, not doing.
// ============================================================================
function StationeryBlock() {
  return (
    <div
      style={{
        background: "var(--color-argent)",
        color: "var(--color-ink)",
        borderRadius: "16px",
        border: "1px solid rgba(11,11,11,0.12)",
        padding: "clamp(24px, 3vw, 36px)",
      }}
    >
      <ExampleBadge>Specimen · Newsroom stationery</ExampleBadge>
      <p
        className="max-w-2xl"
        style={{
          fontFamily: "'Geist', sans-serif",
          fontSize: "14px",
          lineHeight: 1.55,
          color: "rgba(11,11,11,0.6)",
          margin: "12px 0 24px",
        }}
      >
        The Flare business card is Newsroom-scoped — press officers, events, broadcast
        communications. Iron chrome stays the default working card across the org (see Applied
        footer); this one rides when Guardian is speaking, not doing.
      </p>
      <div className="grid gap-4 md:grid-cols-2">
        <BizCard ground="flare" />
      </div>
    </div>
  );
}

function ExampleBadge({ children }: { children: ReactNode }) {
  return (
    <p
      className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em]"
      style={{
        color: "rgba(11,11,11,0.55)",
        fontVariationSettings: '"wght" 600',
        margin: 0,
      }}
    >
      {children}
    </p>
  );
}

function SectionDivider() {
  return (
    <div
      aria-hidden="true"
      style={{
        height: 0,
        borderTop: "1px solid rgba(11,11,11,0.12)",
        margin: "32px 0",
      }}
    />
  );
}

// NewsroomSpecList — plain Argent-on-Argent spec recess for Newsroom.
// Deliberately not the shared Colophon primitive, which paints Vellum
// (cream) and would reintroduce the warm recess Newsroom stays off.
type NewsroomSpecRow = {
  readonly label: string;
  readonly value: string;
  readonly note?: string;
};

function NewsroomSpecList({
  rows,
  footer,
}: {
  rows: readonly NewsroomSpecRow[];
  footer?: ReactNode;
}) {
  return (
    <div
      data-block="newsroom-spec"
      style={{
        background: "var(--color-argent)",
        border: "1px solid rgba(11,11,11,0.12)",
        borderRadius: "10px",
        padding: "28px 32px",
        color: "var(--color-ink)",
      }}
    >
      <div
        style={{
          fontFamily: "'Geist Mono', ui-monospace, monospace",
          fontWeight: 600,
          fontVariationSettings: '"wght" 600',
          fontSize: "10px",
          letterSpacing: "0.18em",
          textTransform: "uppercase",
          color: "rgba(11,11,11,0.55)",
          marginBottom: "16px",
        }}
      >
        Newsroom · Specifications
      </div>
      <dl
        style={{
          display: "grid",
          gridTemplateColumns: "minmax(160px, 1fr) minmax(0, 2fr)",
          columnGap: "28px",
          rowGap: "10px",
          margin: 0,
          fontFamily: "'Geist', sans-serif",
          fontSize: "13px",
          lineHeight: 1.55,
        }}
      >
        {rows.map((row) => (
          <div key={row.label} style={{ display: "contents" }}>
            <dt style={{ color: "rgba(11,11,11,0.6)" }}>{row.label}</dt>
            <dd style={{ margin: 0 }}>
              <span
                style={{
                  fontFamily: "'Geist Mono', ui-monospace, monospace",
                  fontSize: "12px",
                  letterSpacing: "0.04em",
                  color: "var(--color-ink)",
                }}
              >
                {row.value}
              </span>
              {row.note ? (
                <span
                  style={{
                    color: "rgba(11,11,11,0.6)",
                    marginLeft: "12px",
                    fontSize: "12px",
                  }}
                >
                  {row.note}
                </span>
              ) : null}
            </dd>
          </div>
        ))}
      </dl>
      {footer ? (
        <div
          style={{
            marginTop: "18px",
            paddingTop: "14px",
            borderTop: "1px solid rgba(11,11,11,0.10)",
            fontFamily: "'Geist', sans-serif",
            fontSize: "12px",
            lineHeight: 1.5,
            color: "rgba(11,11,11,0.6)",
          }}
        >
          {footer}
        </div>
      ) : null}
    </div>
  );
}
