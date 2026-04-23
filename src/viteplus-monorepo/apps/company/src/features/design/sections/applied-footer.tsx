import type { CSSProperties } from "react";
import { Lockup } from "@forge-metal/brand";
import { Section } from "../section-shell";
import { BizCard, LINE, ScrimLabel, sectionMeta } from "../shared";

// ============================================================================
// 07 — Applied · photography · scrim
// ============================================================================
function SectionPhotography() {
  const meta = sectionMeta("photography");
  const ground: CSSProperties = {
    position: "absolute",
    inset: 0,
    background: `
      radial-gradient(ellipse 70% 35% at 25% 82%, rgba(240, 230, 205, 0.82) 0%, rgba(220, 215, 205, 0.30) 42%, transparent 72%),
      radial-gradient(ellipse 90% 50% at 60% 40%, rgba(210, 220, 230, 0.32) 0%, transparent 65%),
      linear-gradient(180deg, #3b465c 0%, #2a3547 22%, #1a2332 48%, #b7b09a 80%, #e5dcc3 100%)
    `,
  };
  const card: CSSProperties = {
    position: "relative",
    borderRadius: "14px",
    overflow: "hidden",
    aspectRatio: "16 / 10",
    border: `1px solid ${LINE}`,
  };
  const photoMark: CSSProperties = {
    position: "absolute",
    left: "calc(64px * 0.45)",
    bottom: "calc(64px * 0.45)",
    zIndex: 2,
  };
  return (
    <Section
      meta={meta}
      lede={
        <>
          Photography and video break the Iron canvas rule by definition — the ground is whatever
          the image contains. The mark still reads as Argent, but it now needs a floor: an iron
          scrim gradient that guarantees ≥ 3:1 contrast on the wings, regardless of what the camera
          saw.
        </>
      }
    >
      <div className="grid gap-4 md:grid-cols-2">
        <div style={card}>
          <div style={ground} />
          <div style={photoMark}>
            <Lockup size="md" wordmarkColor="var(--color-argent)" />
          </div>
          <ScrimLabel kind="no">Without scrim · fails</ScrimLabel>
        </div>
        <div style={card}>
          <div style={ground} />
          <div
            aria-hidden="true"
            style={{
              position: "absolute",
              inset: 0,
              pointerEvents: "none",
              background: `linear-gradient(180deg,
                rgba(14, 14, 14, 0.00) 0%,
                rgba(14, 14, 14, 0.20) 45%,
                rgba(14, 14, 14, 0.75) 90%,
                rgba(14, 14, 14, 0.90) 100%)`,
            }}
          />
          <div style={photoMark}>
            <Lockup size="md" wordmarkColor="var(--color-argent)" />
          </div>
          <ScrimLabel kind="yes">With scrim · 3:1 floor</ScrimLabel>
        </div>
      </div>
    </Section>
  );
}

// ============================================================================
// 08 — Applied · business cards
//
// Under the three-room model there is a single working card, carried by
// everyone: iron ground, argent lockup (bare cropped wings). The companion Flare
// card retired with the Company treatment — Flare cards appeared on every
// treatment's Applied footer (including Letters), violating the "Flare never
// appears outside Newsroom surfaces" rule. Newsroom-specific stationery is a
// separate future specimen and will live in its own Newsroom-scoped block
// when it does.
// ============================================================================
function SectionBusinessCards() {
  const meta = sectionMeta("business-cards");
  return (
    <Section
      meta={meta}
      lede={
        <>
          The working card, carried by everyone. Iron ground, argent wings cropped tight to the
          glyph — the same mark the live console wears, handed across a table.
        </>
      }
    >
      <div className="grid gap-4 md:grid-cols-2">
        <BizCard ground="iron" />
      </div>
    </Section>
  );
}

// ============================================================================
// AppliedFooter — the cross-treatment rules that sit at the foot of every
// treatment page. A small separator + heading ("Cross-treatment rules ·
// Applied") introduces the pair so the reader knows they have left the
// per-treatment flow and entered shared Applied territory.
// ============================================================================
export function AppliedFooter() {
  return (
    <>
      <div
        aria-hidden="true"
        style={{
          height: 0,
          borderTop: `1px solid ${LINE}`,
          margin: "48px 0 24px",
        }}
      />
      <div
        style={{
          fontFamily: "'Geist Mono', ui-monospace, monospace",
          fontWeight: 600,
          fontVariationSettings: '"wght" 600',
          fontSize: "11px",
          lineHeight: 1,
          letterSpacing: "0.18em",
          textTransform: "uppercase",
          color: "var(--treatment-muted-meta)",
          margin: "0 0 24px",
        }}
      >
        Cross-treatment rules · Applied
      </div>
      <SectionPhotography />
      <SectionBusinessCards />
    </>
  );
}
