import { Lockup, WingsEmboss } from "@forge-metal/brand";
import { RulesRow, Section } from "../section-shell";
import { Colophon } from "../colophon";
import { LINE, sectionMeta, Surface } from "../shared";
import {
  TreatmentLockupLadder,
  TreatmentMarkCard,
  TreatmentPalette,
  TreatmentSignature,
  TreatmentTypeLadder,
} from "../treatments";

// ============================================================================
// 02 — Treatments · Newsroom
//
// Newsroom is a Paper reading ground plus a bounded Flare hero band — the
// "Letters body + broadcast band" formula. Every demonstrative block below
// paints on Paper; Flare appears once, inside the hero-band Surface. This
// keeps the teaching honest: a reader experiences the treatment exactly the
// way a real Newsroom page ships, not as a sea of acid green.
// ============================================================================
export function SectionNewsroom() {
  const meta = sectionMeta("newsroom");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Paper ground under the reading text; a single bounded Flare band carries the broadcast.
          The moment Guardian chooses to be noticed — investor deck covers, billboards, social hero
          images, recruiting posters, conference backdrops, merch — appears inside that band.{" "}
          <b>
            OG cards, press hero imagery, and every share preview ride under this treatment by
            default
          </b>
          ; acid green is how Guardian appears in someone else&apos;s feed, never the ground they
          read on.
        </>
      }
    >
      <TreatmentPalette
        roles={{
          ground: {
            name: "Paper",
            hex: "#F6F4ED",
            note: "Reading canvas, same as Letters.",
            chipStyle: { boxShadow: `inset 0 0 0 1px ${LINE}` },
          },
          accent: {
            name: "Flare",
            hex: "#CCFF00",
            pantone: "Pantone 389 C",
            note: "Hero band · OG card · billboard. Never the ground.",
          },
          mark: {
            name: "Argent",
            hex: "#FFFFFF",
            note: "Wings inside an ink emboss medallion.",
            chipStyle: { boxShadow: "inset 0 0 0 1px rgba(11,11,11,0.18)" },
          },
          muted: {
            name: "Stone",
            hex: "#0B0B0B",
            note: "Bylines, kickers, and meta · Ink at 0.7 / 0.6 / 0.55 opacity.",
            chipStyle: { background: "rgba(11,11,11,0.7)" },
          },
        }}
        rule={
          <>
            One Flare surface per composition, and it has to be pulling weight — the lead bulletin,
            the share preview, the billboard. Anything quieter belongs on Paper.{" "}
            <b>Bordeaux never appears on Newsroom</b> and{" "}
            <b>Flare never appears outside Newsroom surfaces</b>: the two accents never trade
            places.
          </>
        }
      />

      {/* Mark specimen + Type ladder — Newsroom's "rules" pair. The mark card
          shows the emboss medallion against Paper (the Newsroom page ground),
          matching what the reader actually sees in the AppChrome. The type
          ladder sits right — still display-weight Fraunces because Newsroom
          copy never descends into body prose. */}
      <RulesRow>
        <TreatmentMarkCard
          groundVar="var(--color-paper)"
          rows={[
            { label: "Argent · Paper · emboss", value: "Newsroom", emphasise: "name" },
            { label: "ground", value: "#F6F4ED", emphasise: "hex" },
            { label: "emboss", value: "#0B0B0B", emphasise: "hex" },
            { label: "wings", value: "#FFFFFF", emphasise: "hex" },
          ]}
        >
          <WingsEmboss style={{ width: "58%", height: "auto" }} />
        </TreatmentMarkCard>
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
                color: "var(--color-ink)",
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
                color: "var(--color-ink)",
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
              belongs under Letters — Newsroom is broadcast, not reading.
            </>
          }
        />
      </RulesRow>
      <div style={{ marginBottom: "16px" }}>
        {/* Lockup ladder — the emboss medallion against Paper. The reader
            inspects the wordmark proportions here without Flare fighting for
            attention; Flare is reserved for the band below. */}
        <TreatmentLockupLadder
          groundVar="var(--color-paper)"
          accentColor="var(--color-bordeaux)"
          metaColor="rgba(11,11,11,0.72)"
          footerColor="rgba(11,11,11,0.55)"
          borderColor="rgba(11,11,11,0.14)"
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
              The wordmark sets in Ink on Paper; the emboss medallion carries the wings so the
              argent never reads directly against the ground. The same medallion is what the band
              below holds against Flare.
            </>
          }
        />
      </div>

      {/* The hero band — the one Flare specimen on the page. Bounded to 480 px
          and clipped, because the teaching point is how Newsroom COMPOSES
          (Paper around, Flare inside) not how much Flare can fit on one
          screen. */}
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
            { label: "Ground", value: "#F6F4ED", note: "Paper · body" },
            { label: "Accent", value: "#CCFF00", note: "Pantone 389 C · Flare · band only" },
            { label: "Type", value: "#0B0B0B", note: "Ink on Paper" },
            { label: "Mark", value: "WingsEmboss", note: "Argent wings in ink medallion" },
            { label: "Display", value: "Fraunces", note: "opsz 144 · SOFT 30 · -0.028em" },
            { label: "Kicker", value: "Geist Mono", note: "11 / 1 / +180 · 600 · UPPER" },
            { label: "Surfaces", value: "Bulletin band · OG card · billboard" },
          ]}
          footer={
            <>
              Flare is allowed on exactly three Newsroom surfaces: a bounded hero band, a 1200×630
              OG card, and a 16:9 billboard. Everything else on a Newsroom page reads as Ink on
              Paper — Flare is the ground a reader glances at, not the ground they read on.
            </>
          }
        />
      </div>
      <div style={{ marginTop: "24px" }}>
        {/* Email signature specimen — the one place Flare returns, because
            an emailed signature IS a small broadcast card. Bounded card, small
            surface; does not violate the "one Flare band" rule for the page
            body because it sits as a peer demonstration, not a page layout. */}
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
