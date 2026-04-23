import { Lockup, WingsEmboss } from "@forge-metal/brand";
import { RulesRow, Section } from "../section-shell";
import { Colophon } from "../colophon";
import { sectionMeta, Surface } from "../shared";
import {
  TreatmentLockupLadder,
  TreatmentMarkCard,
  TreatmentPalette,
  TreatmentSignature,
  TreatmentTypeLadder,
} from "../treatments";

// ============================================================================
// 05 — Treatments · Newsroom
// ============================================================================
export function SectionNewsroom() {
  const meta = sectionMeta("newsroom");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Flare ground, wings in a circular ink emboss, Fraunces at display weight. The Newsroom
          treatment is the moment the brand chooses to be noticed — investor deck covers,
          billboards, social hero images, recruiting posters, conference backdrops, merch.{" "}
          <b>
            OG cards, press hero imagery, and every share preview ride under this treatment by
            default
          </b>{" "}
          — acid green is how Guardian appears in someone else's feed.
        </>
      }
    >
      <TreatmentPalette
        roles={{
          ground: {
            name: "Flare",
            hex: "#CCFF00",
            pantone: "Pantone 389 C",
            note: "The broadcast canvas.",
          },
          accent: {
            name: "Iron",
            hex: "#0E0E0E",
            note: "Inverted primary action · ink-dark button.",
          },
          mark: {
            name: "Argent",
            hex: "#FFFFFF",
            note: "Wings inside a circular ink emboss.",
          },
          // Newsroom genuinely has no muted register — it is broadcast, not
          // reading. The palette's fourth column renders a "not used" cell
          // so the absence is visible.
        }}
        rule={
          <>
            One action, loud and single; everything else reads in ink so the ground does the
            shouting. Bordeaux and Amber never appear here — Newsroom is Flare's surface alone.
          </>
        }
      />

      {/* Mark specimen + Type ladder — Newsroom's "rules" pair. The emboss
          mark (wings-in-ink-medallion) is the only variant that reads over
          Flare without a legibility fight; the display-only type ladder
          sits right. The full lockup ladder renders full-width below so
          the 96 px emboss mark can breathe. */}
      <RulesRow>
        <TreatmentMarkCard
          groundVar="var(--color-flare)"
          rows={[
            { label: "Argent · Flare · emboss", value: "Newsroom", emphasise: "name" },
            { label: "ground", value: "#CCFF00", emphasise: "hex" },
            { label: "emboss", value: "#0B0B0B", emphasise: "hex" },
            { label: "wings", value: "#FFFFFF", emphasise: "hex" },
          ]}
        >
          <WingsEmboss style={{ width: "58%", height: "auto" }} />
        </TreatmentMarkCard>
        {/* Newsroom type is display-only. No body copy — Flare is too loud
            a ground to read paragraphs on. */}
        <TreatmentTypeLadder
          rows={[
            {
              sample: "We build where software meets the real world.",
              role: "display · hero",
              spec: "Fraunces / 64 / 1.00 / -28 · opsz 144 · SOFT 30",
              sampleSizePx: 64,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 144, "SOFT" 30',
                fontWeight: 400,
                fontSize: "64px",
                lineHeight: 1.0,
                letterSpacing: "-0.028em",
                color: "var(--color-type-iron)",
              },
            },
            {
              sample: "Applied intelligence, built in Seattle.",
              role: "h1 · poster",
              spec: "Fraunces / 40 / 1.05 / -20 · opsz 96",
              sampleSizePx: 40,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 96, "SOFT" 20',
                fontWeight: 400,
                fontSize: "40px",
                lineHeight: 1.05,
                letterSpacing: "-0.02em",
              },
            },
            {
              sample: "SEATTLE · EST. 2026",
              role: "kicker · upper",
              spec: "Geist Mono / 11 / 1 / +180 · 600 · UPPER",
              sampleSizePx: 11,
              sampleStyle: {
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                fontSize: "11px",
                lineHeight: 1,
                letterSpacing: "0.18em",
                textTransform: "uppercase",
                color: "var(--treatment-muted)",
              },
            },
          ]}
          caption={
            <>
              Newsroom type stops at display and its kicker. If a surface needs body prose, it
              belongs under Letters or Company; Flare is too loud a ground to read paragraphs on.
            </>
          }
        />
      </RulesRow>
      <div style={{ marginBottom: "16px" }}>
        {/* Lockup ladder at full width — the 96 px emboss mark needs the
            horizontal room and meta column readability on Flare is best
            when the card spans the full content width. */}
        <TreatmentLockupLadder
          groundVar="var(--color-flare)"
          // Meta column has to fight Flare's high luminance; switch to
          // ink-muted (Stone) for meta/footer and iron for the gap accent
          // so "14.6 px" doesn't go Flare-on-Flare.
          accentColor="var(--color-iron)"
          metaColor="rgba(11,11,11,0.72)"
          footerColor="rgba(11,11,11,0.55)"
          borderColor="rgba(11,11,11,0.18)"
          rows={[
            {
              size: "lg",
              variant: "emboss",
              wordmarkColor: "var(--color-ink)",
              markPx: 96,
              gap: "18 px",
              role: "ceiling",
            },
            {
              size: "md",
              variant: "emboss",
              wordmarkColor: "var(--color-ink)",
              markPx: 52,
              gap: "14.6 px",
              role: "proportional",
            },
            {
              size: "sm",
              variant: "emboss",
              wordmarkColor: "var(--color-ink)",
              markPx: 28,
              gap: "8 px",
              role: "floor",
            },
          ]}
          footer={
            <>
              On Flare the wordmark sets in Ink, never Argent. The medallion carries the wings so
              the argent never reads directly against the ground.
            </>
          }
        />
      </div>

      {/* Hero-band specimen. Bounded (max 480 px) so the demonstration stays a
          BAND on the Paper page, not an environment. The teaching point is how
          Newsroom composes — ground, mark, display type, one CTA — not how much
          Flare can fit on one screen. */}
      <Surface
        ground="flare"
        style={{
          padding: "clamp(32px, 4.5vw, 56px) clamp(20px, 4vw, 56px)",
          borderRadius: "16px",
          maxHeight: "480px",
          overflow: "hidden",
        }}
      >
        <div style={{ marginBottom: "22px" }}>
          <Lockup size="md" variant="emboss" wordmarkColor="var(--color-ink)" />
        </div>
        <div className="hero-kicker" style={{ color: "rgba(11,11,11,0.7)" }}>
          Seattle · Est. 2026
        </div>
        <h1
          className="hero-h1"
          style={{
            color: "var(--color-ink)",
            fontSize: "clamp(32px, 5vw, 52px)",
            margin: "0 0 20px",
          }}
        >
          We build where software meets the real world.
        </h1>
        <div className="hero-cta-row">
          <button
            className="hero-btn primary"
            style={{
              background: "var(--color-iron)",
              color: "var(--color-flare)",
              borderColor: "var(--color-iron)",
            }}
          >
            Read the letters
          </button>
        </div>
      </Surface>
      <div style={{ marginTop: "24px" }}>
        <Colophon
          heading="Newsroom · Specifications"
          rows={[
            { label: "Ground", value: "#CCFF00", note: "Pantone 389 C · Flare" },
            { label: "Type", value: "#0B0B0B", note: "Ink · ink-on-flare" },
            { label: "Mark", value: "WingsEmboss", note: "Argent wings in ink medallion" },
            { label: "Display", value: "Fraunces", note: 'opsz 144 · SOFT 30 · -0.028em' },
            { label: "Kicker", value: "Geist Mono", note: "11 / 1 / +180 · 600 · UPPER" },
            { label: "Surfaces", value: "OG cards · billboards · hero bands" },
          ]}
          footer={
            <>
              Flare is allowed on exactly three Newsroom surfaces: a bounded hero band, a 1200×630
              OG card, and a 16:9 billboard. Any Newsroom body copy sets in Ink on Paper — Flare
              is the ground a reader glances at, not the ground they read on.
            </>
          }
        />
      </div>
      <div style={{ marginTop: "24px" }}>
        {/* Newsroom's card ground is Flare. The treatment's identity is in
            the ground, so the accent marker is dropped — no dot, no rule.
            Mark switches to the black emboss variant (wings-on-ink medallion)
            because Argent-on-Flare fails luminance contrast. */}
        <TreatmentSignature
          variant="newsroom"
          eyebrow="Email signature · Newsroom"
          markVariant="emboss"
          identity={{
            name: "Press Officer Name",
            role: "Communications · Guardian Intelligence",
          }}
          accent={{ hex: "#0b0b0b", style: "none" }}
          contact={{
            email: "press@guardianintelligence.org",
            secondary: "guardianintelligence.org/press",
          }}
        />
      </div>
    </Section>
  );
}
