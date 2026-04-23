import { Lockup, WingsArgent } from "@forge-metal/brand";
import { RulesRow, Section } from "../section-shell";
import { heroStyle, sectionMeta, Surface } from "../shared";
import {
  TreatmentLockupLadder,
  TreatmentMarkCard,
  TreatmentPalette,
  TreatmentSignature,
  TreatmentSizeLadder,
  TreatmentTypeLadder,
} from "../treatments";

// ============================================================================
// 03 — Treatments · Company
// ============================================================================
export function SectionCompany() {
  const meta = sectionMeta("company");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Iron ground. Fraunces for the voice, Geist for the work. The Guardian lockup is present in
          the chrome because this is where a reader meets the brand on the record — the landing, the
          mission, the press contact. Flare is reserved for a single action; type stays unhurried;
          the wings stay Argent.
        </>
      }
    >
      <TreatmentPalette
        roles={{
          ground: { name: "Iron", hex: "#0E0E0E", note: "The corporate canvas." },
          accent: {
            name: "Flare",
            hex: "#CCFF00",
            pantone: "Pantone 389 C",
            note: "One action per screen.",
          },
          mark: { name: "Argent", hex: "#FFFFFF", note: "Wings, invariant." },
          muted: {
            name: "Ash",
            hex: "#F5F5F5",
            note: "Body & meta · opacity 0.82 / 0.72 / 0.60 / 0.55.",
            chipStyle: { background: "rgba(245,245,245,0.72)" },
          },
        }}
        rule={
          <>
            Flare is Guardian's official accent colour — the hue that stands for the company in the
            outside world. On Company surfaces it appears on exactly one element per screen: the
            primary action, an accent word, or a hairline. Never two.
          </>
        }
      />

      {/* Mark specimen + Type ladder — the "rules" exhibit. At ≥ lg the mark
          carrier sits left (narrow), the type ladder right (wide). The size
          and lockup ladders below are supplementary and keep full width so
          the Guardian lockup can breathe at 96 px. */}
      <RulesRow>
        <TreatmentMarkCard
          groundVar="var(--color-iron)"
          rows={[
            { label: "Argent · Iron", value: "Company", emphasise: "name" },
            { label: "ground", value: "#0E0E0E", emphasise: "hex" },
            { label: "wings", value: "#FFFFFF", emphasise: "hex" },
          ]}
        >
          <WingsArgent style={{ width: "64%", height: "auto" }} cropped />
        </TreatmentMarkCard>
        {/* Type ladder — Company's flavour: the full set from display to
            badge, because Company is where every typographic register of
            the firm shows up (landing → mission → body copy → legal meta).
            Other treatments ship leaner ladders (Workshop drops Fraunces;
            Newsroom is display-only; Letters rebalances Fraunces as body). */}
        <TreatmentTypeLadder
          rows={[
            {
              sample: "The application layer is the product.",
              role: "display · hero",
              spec: "Fraunces / 64 / 1.02 / -25 · opsz 144 · SOFT 30",
              sampleSizePx: 64,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 144, "SOFT" 30',
                fontWeight: 400,
                fontSize: "64px",
                lineHeight: 1.02,
                letterSpacing: "-0.025em",
              },
            },
            {
              sample: "Toward a million solo-founded companies.",
              role: "h1 · page",
              spec: "Fraunces / 48 / 1.05 / -20 · opsz 96 · SOFT 20",
              sampleSizePx: 48,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 96, "SOFT" 20',
                fontWeight: 400,
                fontSize: "48px",
                lineHeight: 1.05,
                letterSpacing: "-0.02em",
              },
            },
            {
              sample: "Compute, integrations, and founder tooling.",
              role: "h2 · section",
              spec: "Fraunces / 32 / 1.1 / -18 · opsz 72",
              sampleSizePx: 32,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 72',
                fontWeight: 400,
                fontSize: "32px",
                lineHeight: 1.1,
                letterSpacing: "-0.018em",
              },
            },
            {
              sample:
                "Guardian Intelligence is an American applied intelligence firm. We build the compute, the integrations, and the founder tooling that make a one-person billion-ARR company an engineering target rather than a slogan.",
              role: "body",
              spec: "Geist / 16 / 1.55 · Regular",
              sampleSizePx: 16,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 400,
                fontSize: "16px",
                lineHeight: 1.55,
              },
            },
            {
              sample: "Secondary copy, metadata, form help text, caption.",
              role: "small · ash",
              spec: "Geist / 13 / 1.5 · Regular · Ash default",
              sampleSizePx: 13,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 400,
                fontSize: "13px",
                lineHeight: 1.5,
                color: "var(--treatment-muted)",
              },
            },
            {
              sample: "Release № 0.4.1 · Shipped 19 Apr 2026",
              role: "badge / eyebrow",
              spec: "Geist / 10 / 1 / +180 · Medium · UPPER",
              sampleSizePx: 10,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 500,
                fontSize: "10px",
                lineHeight: 1,
                letterSpacing: "0.18em",
                textTransform: "uppercase",
              },
            },
          ]}
        />
      </RulesRow>
      <div style={{ display: "grid", gap: "16px", marginBottom: "16px" }}>
        <TreatmentSizeLadder />
        <TreatmentLockupLadder
          rows={[
            { size: "lg", markPx: 96, gap: "18 px", role: "ceiling" },
            { size: "md", markPx: 52, gap: "14.6 px", role: "proportional" },
            { size: "sm", markPx: 28, gap: "8 px", role: "floor" },
          ]}
          footer={
            <>
              Clear space · 1 × wing-unit outside the lockup ·{" "}
              <span style={{ color: "var(--treatment-muted)" }}>
                gap clamp(8, 0.28 · mark-h, 18) inside
              </span>
            </>
          }
        />
      </div>

      <Surface
        ground="iron"
        style={{ padding: "clamp(32px, 5vw, 72px) clamp(20px, 4vw, 56px)", borderRadius: "16px" }}
      >
        <div style={{ marginBottom: "28px" }}>
          <Lockup size="md" />
        </div>
        <div className="hero-kicker">
          An American applied intelligence company · Est. 2026 · Seattle, Washington
        </div>
        <h1 className="hero-h1">
          The world needs your business to succeed, and we're here to help.
        </h1>
        <div className="hero-cta-row">
          <button className="hero-btn primary">Request access</button>
          <button className="hero-btn ghost">Read the letters →</button>
        </div>
        <div className="mission-block">
          <p>
            Every founder spends the first year on the same dozen systems — identity, billing,
            analytics, email, infrastructure, security, the thousand edges where a real company
            touches the real world. None of it is what you started the company to build. We build
            the reference architecture for all of it — open-source, documented, and clean enough
            that one founder with <b>Claude Code</b> can run a billion-dollar company.
          </p>
          <p className="mission-closer">
            If you want to do something good for the world, we want to make it easy.
          </p>
        </div>
      </Surface>
      <div style={{ marginTop: "24px" }}>
        <TreatmentSignature
          variant="company"
          eyebrow="Email signature · Company"
          markVariant="chip"
          identity={{ name: "Founder Name", role: "Founder · Guardian Intelligence" }}
          accent={{ hex: "#CCFF00", style: "hairline" }}
          contact={{
            email: "founder@guardianintelligence.org",
            secondary: "guardianintelligence.org",
          }}
        />
      </div>
      <style>{heroStyle}</style>
    </Section>
  );
}
