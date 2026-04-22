import type { CSSProperties, ReactNode } from "react";
import { Lockup, WingsArgent, WingsChip, WingsEmboss } from "@forge-metal/brand";
import { DESIGN_SECTIONS } from "~/lib/design-nav";
import { RulesRow, Section } from "./section-shell";
import {
  Nameplate,
  SignatureStatusBadge,
  TreatmentLockupLadder,
  TreatmentMarkCard,
  TreatmentMastheadLadder,
  TreatmentPalette,
  TreatmentSignature,
  TreatmentSizeLadder,
  TreatmentTypeLadder,
  TreatmentWingsOnlyLadder,
} from "./treatments";

const sectionByID = (id: (typeof DESIGN_SECTIONS)[number]["id"]) =>
  DESIGN_SECTIONS.find((s) => s.id === id)!;

// Hairline colour for dark panels and dark-ground borders inside the Workshop
// dashboard specimen and the treatment Surface wrapper. The treatment
// primitives carry their own internal LINE constants.
const LINE = "#2a2a2f";

// ============================================================================
// Surface — a flat treatment canvas (Iron / Flare / Paper).
// ============================================================================
function Surface({
  ground,
  children,
  className,
  style,
}: {
  readonly ground: "iron" | "flare" | "paper";
  readonly children: ReactNode;
  readonly className?: string;
  readonly style?: CSSProperties;
}) {
  const groundStyle: CSSProperties =
    ground === "iron"
      ? { background: "var(--color-iron)", color: "var(--color-type-iron)" }
      : ground === "flare"
        ? { background: "var(--color-flare)", color: "var(--color-ink)" }
        : { background: "var(--color-paper)", color: "var(--color-ink)" };
  return (
    <div
      className={className}
      style={{
        padding: "48px 40px",
        border: `1px solid ${LINE}`,
        borderRadius: "12px",
        marginBottom: "16px",
        ...groundStyle,
        ...style,
      }}
    >
      {children}
    </div>
  );
}

// ============================================================================
// 03 — Treatments · Company
// ============================================================================
function SectionCompany() {
  const meta = sectionByID("company");
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
                color: "var(--muted)",
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
              <span style={{ color: "var(--muted)" }}>gap clamp(8, 0.28 · mark-h, 18) inside</span>
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

// ============================================================================
// 04 — Treatments · Workshop
//
// Workshop is the productivity treatment. Two load-bearing rules:
//   1. No Fraunces. Everything sets in Geist + Geist Mono.
//   2. No Guardian wordmark in the chrome. The operator's customer is
//      thinking about their tenant, not about Guardian. Wings persist (22 px)
//      as a quiet identity anchor but never lock up with text.
// Amber is the workshop accent; Flare is banned.
// ============================================================================
function SectionWorkshop() {
  const meta = sectionByID("workshop");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Inside the product, the chrome disappears. Everything — navigation, controls, data, code —
          sets in Geist and Geist Mono; Fraunces is absent. The chrome carries the tenant's name,
          not ours — a founder working here is thinking about their own company, not about Guardian.
          Wings persist at 22 px as a quiet identity anchor. The one accent is{" "}
          <b style={{ color: "var(--color-amber)" }}>Amber</b>: primary actions, live states,
          positive signals. Flare is banned from workshop surfaces.
        </>
      }
    >
      <TreatmentPalette
        roles={{
          ground: { name: "Iron", hex: "#0E0E0E", note: "The workshop floor." },
          accent: {
            name: "Amber",
            hex: "#F79326",
            pantone: "Pantone 715 C",
            note: "Primary actions, live state, positive signals.",
          },
          mark: { name: "Argent", hex: "#FFFFFF", note: "Wings only · 22 px in chrome." },
          muted: {
            name: "Ash",
            hex: "#F5F5F5",
            note: "Body & meta · Geist + Geist Mono.",
            chipStyle: { background: "rgba(245,245,245,0.72)" },
          },
        }}
        rule={
          <>
            Amber reads as <i>work is happening here</i> — a nod to Bloomberg Terminal's amber
            phosphor. <b>Flare is banned from Workshop</b> and{" "}
            <b>Amber never ships outside Workshop</b>; the two accents trade places at the chrome
            boundary so an operator always knows which context they are inside.
          </>
        }
      />

      {/* Mark specimen + Type ladder — Workshop's "rules" pair. Mark carrier
          left (wings only — no wordmark ever), type ladder right. The
          wings-only size ladder stays full-width below so it can show the
          full 64 → 16 descent without cramping. */}
      <RulesRow>
        <TreatmentMarkCard
          groundVar="var(--color-iron)"
          rows={[
            { label: "Argent · Iron · wings only", value: "Workshop", emphasise: "name" },
            { label: "ground", value: "#0E0E0E", emphasise: "hex" },
            { label: "wings", value: "#FFFFFF", emphasise: "hex" },
          ]}
        >
          <WingsArgent style={{ width: "56%", height: "auto" }} cropped />
        </TreatmentMarkCard>
        <TreatmentTypeLadder
          rows={[
            {
              sample: "Sandbox execution",
              role: "h3 · ui · workshop",
              spec: "Geist / 20 / 1.3 / -10 · SemiBold",
              sampleSizePx: 20,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 600,
                fontSize: "20px",
                lineHeight: 1.3,
                letterSpacing: "-0.01em",
              },
            },
            {
              sample:
                "14 active across 4 tenants · 3 h 22 m median lease · 99.98% attestation rate.",
              role: "body",
              spec: "Geist / 14 / 1.5 · Regular",
              sampleSizePx: 14,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 400,
                fontSize: "14px",
                lineHeight: 1.5,
              },
            },
            {
              sample: "tenant · region · workload · status",
              role: "small · ash",
              spec: "Geist / 12 / 1.5 · Regular · Ash",
              sampleSizePx: 12,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 400,
                fontSize: "12px",
                lineHeight: 1.5,
                color: "var(--muted)",
              },
            },
            {
              sample: "0x41e9f2a  attest=true  lease=3h22m  region=us-east-1",
              role: "mono",
              spec: "Geist Mono / 12 / 1.5 · Regular",
              sampleSizePx: 12,
              sampleStyle: {
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                fontWeight: 400,
                fontSize: "12px",
                lineHeight: 1.5,
                color: "var(--muted)",
              },
            },
            {
              sample: "LIVE · PAGEABLE · US-EAST-1",
              role: "badge · pageable",
              spec: "Geist Mono / 10 / 1 / +180 · 600 · UPPER",
              sampleSizePx: 10,
              sampleStyle: {
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                fontSize: "10px",
                lineHeight: 1,
                letterSpacing: "0.18em",
                textTransform: "uppercase",
                color: "var(--color-amber)",
              },
            },
          ]}
          caption={
            <>
              Workshop declines Fraunces entirely. If an editor ever asks for a serif inside the
              product, that's a smell: the surface probably belongs under Letters or Newsroom, not
              Workshop.
            </>
          }
        />
      </RulesRow>
      <div style={{ marginBottom: "16px" }}>
        <TreatmentWingsOnlyLadder
          note={
            <>
              22 px is the size the live console chrome ships. Below 22 px the glyph starts to lose
              its lower-wing tip at typical display DPI; above 64 px the wings feel like a logo
              looking for a sentence.
            </>
          }
        />
      </div>
      <div
        style={{
          background: "var(--color-iron)",
          color: "var(--color-type-iron)",
          borderRadius: "12px",
          overflow: "hidden",
          border: `1px solid ${LINE}`,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "14px 20px",
            borderBottom: `1px solid ${LINE}`,
            flexWrap: "wrap",
            gap: "12px",
          }}
        >
          {/* Header identity: wings + tenant name in Geist. No Fraunces,
              no "Guardian" wordmark. */}
          <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
            <WingsArgent style={{ width: "22px", height: "22px" }} />
            <span
              style={{
                fontFamily: "'Geist', sans-serif",
                fontWeight: 500,
                fontSize: "14px",
                letterSpacing: "-0.005em",
              }}
            >
              acme-corp
            </span>
            <span
              style={{
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                fontSize: "11px",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                color: "var(--muted-faint)",
                letterSpacing: "0.08em",
              }}
            >
              / production
            </span>
          </div>
          <nav
            style={{
              display: "flex",
              gap: "24px",
              fontFamily: "'Geist', sans-serif",
              fontSize: "13px",
            }}
          >
            <span style={{ color: "var(--color-type-iron)" }}>Overview</span>
            <span style={{ color: "var(--muted)" }}>Compute</span>
            <span style={{ color: "var(--muted)" }}>Integrations</span>
            <span style={{ color: "var(--muted)" }}>Leases</span>
            <span style={{ color: "var(--muted)" }}>Billing</span>
          </nav>
          <div style={{ display: "flex", gap: "10px", alignItems: "center" }}>
            <span
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: "6px",
                fontSize: "10px",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                letterSpacing: "0.16em",
                textTransform: "uppercase",
                padding: "4px 10px",
                borderRadius: "999px",
                border: "1px solid rgba(245,245,245,0.2)",
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                color: "rgba(245,245,245,0.72)",
              }}
            >
              <span
                aria-hidden="true"
                style={{
                  width: "6px",
                  height: "6px",
                  borderRadius: "50%",
                  background: "var(--color-amber)",
                  boxShadow: "0 0 0 2px rgba(247,147,38,0.22)",
                }}
              />
              Live
            </span>
            <button
              style={{
                fontFamily: "'Geist', sans-serif",
                fontWeight: 500,
                fontSize: "13px",
                padding: "8px 14px",
                borderRadius: "6px",
                border: "1px solid var(--color-amber)",
                background: "var(--color-amber)",
                color: "var(--color-ink)",
                cursor: "pointer",
              }}
            >
              Deploy
            </button>
          </div>
        </div>
        <div className="workshop-body">
          <aside className="workshop-aside">
            <style>{`
              .workshop-body {
                display: grid;
                grid-template-columns: 1fr;
                min-height: 420px;
              }
              .workshop-aside {
                border-bottom: 1px solid ${LINE};
                padding: 16px 20px;
                font-family: 'Geist', sans-serif;
                font-size: 13px;
              }
              @media (min-width: 768px) {
                .workshop-body { grid-template-columns: 220px 1fr; }
                .workshop-aside {
                  border-right: 1px solid ${LINE};
                  border-bottom: 0;
                  padding: 20px 16px;
                }
              }
            `}</style>
            <div
              style={{
                color: "var(--muted-faint)",
                fontSize: "10px",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                letterSpacing: "0.16em",
                textTransform: "uppercase",
                margin: "0 8px 8px",
              }}
            >
              Workspace
            </div>
            {[
              { label: "Overview", active: true },
              { label: "Sandboxes" },
              { label: "Leases" },
              { label: "Attestations" },
            ].map((item) => (
              <span
                key={item.label}
                style={{
                  display: "block",
                  padding: "8px 10px",
                  borderRadius: "6px",
                  color: item.active ? "var(--color-type-iron)" : "var(--muted)",
                  background: item.active ? "#1c1c20" : "transparent",
                }}
              >
                {item.label}
              </span>
            ))}
            <div
              style={{
                color: "var(--muted-faint)",
                fontSize: "10px",
                fontWeight: 600,
                fontVariationSettings: '"wght" 600',
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                letterSpacing: "0.16em",
                textTransform: "uppercase",
                margin: "20px 8px 8px",
              }}
            >
              Account
            </div>
            {["Integrations", "Billing", "Settings"].map((label) => (
              <span
                key={label}
                style={{
                  display: "block",
                  padding: "8px 10px",
                  borderRadius: "6px",
                  color: "var(--muted)",
                }}
              >
                {label}
              </span>
            ))}
          </aside>
          <div style={{ padding: "clamp(20px, 3vw, 28px) clamp(20px, 3vw, 32px)", minWidth: 0 }}>
            <h2
              style={{
                fontFamily: "'Geist', sans-serif",
                fontWeight: 600,
                fontSize: "clamp(20px, 2.4vw, 24px)",
                lineHeight: 1.2,
                letterSpacing: "-0.01em",
                margin: "0 0 6px",
                color: "var(--color-type-iron)",
                textTransform: "none",
              }}
            >
              Production sandboxes
            </h2>
            <p
              style={{
                color: "var(--muted)",
                fontFamily: "'Geist', sans-serif",
                fontSize: "14px",
                margin: "0 0 20px",
              }}
            >
              14 active across 4 tenants · 3 h 22 m median lease · 99.98% attestation rate
            </p>
            <div style={{ overflowX: "auto" }}>
              <table
                style={{
                  width: "100%",
                  borderCollapse: "collapse",
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "13px",
                  minWidth: "520px",
                }}
              >
                <thead>
                  <tr>
                    {["Tenant", "Region", "Workload", "Lease", "Status"].map((col, i) => (
                      <th
                        key={col}
                        style={{
                          padding: "12px 14px",
                          textAlign: i === 3 ? "right" : "left",
                          borderBottom: `1px solid ${LINE}`,
                          fontSize: "10px",
                          fontFamily: "'Geist Mono', ui-monospace, monospace",
                          letterSpacing: "0.14em",
                          textTransform: "uppercase",
                          color: "var(--muted-faint)",
                          fontWeight: 600,
                          fontVariationSettings: '"wght" 600',
                        }}
                      >
                        {col}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {[
                    [
                      "acme-corp",
                      "us-east-1",
                      "inference · h100×8",
                      "0x41e9f2a",
                      "● attested",
                      "ok",
                    ],
                    ["hex-labs", "us-east-1", "ci · runner-pool", "0x41e9f2b", "● attested", "ok"],
                    [
                      "lumen-mail",
                      "eu-west-1",
                      "stateful · zfs-pool",
                      "0x41e9f2c",
                      "○ draining",
                      "warn",
                    ],
                    [
                      "solo-founder",
                      "us-west-2",
                      "editor · agent-vm",
                      "0x41e9f2d",
                      "● attested",
                      "ok",
                    ],
                  ].map((row) => (
                    <tr key={row[0]}>
                      <td style={{ padding: "12px 14px", borderBottom: `1px solid ${LINE}` }}>
                        {row[0]}
                      </td>
                      <td style={{ padding: "12px 14px", borderBottom: `1px solid ${LINE}` }}>
                        {row[1]}
                      </td>
                      <td style={{ padding: "12px 14px", borderBottom: `1px solid ${LINE}` }}>
                        {row[2]}
                      </td>
                      <td
                        style={{
                          padding: "12px 14px",
                          borderBottom: `1px solid ${LINE}`,
                          fontFamily: "'Geist Mono', ui-monospace, monospace",
                          fontSize: "12px",
                          color: "var(--color-type-iron)",
                          textAlign: "right",
                        }}
                      >
                        {row[3]}
                      </td>
                      <td
                        style={{
                          padding: "12px 14px",
                          borderBottom: `1px solid ${LINE}`,
                          color: row[5] === "ok" ? "var(--color-amber)" : "#f0c74f",
                          fontWeight: 500,
                        }}
                      >
                        {row[4]}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <pre
              style={{
                background: "#050505",
                color: "#d4d4d4",
                padding: "16px 18px",
                margin: "20px 0 0",
                borderRadius: "8px",
                font: '12px/1.6 "Geist Mono", ui-monospace, monospace',
                overflow: "auto",
                border: `1px solid ${LINE}`,
              }}
            >
              <span style={{ color: "#5d5a52", fontStyle: "italic" }}>
                {"// Deploy a sandbox from the Metal CLI."}
              </span>
              {"\n"}
              <span style={{ color: "#C0C0F2" }}>import</span>
              {" { sandbox } "}
              <span style={{ color: "#C0C0F2" }}>from</span>{" "}
              <span style={{ color: "var(--color-amber)" }}>{`"@metal/compute"`}</span>;{"\n\n"}
              <span style={{ color: "#C0C0F2" }}>await</span> sandbox.run({"{"}
              {"\n"}
              {"  tenant:   "}
              <span style={{ color: "var(--color-amber)" }}>{`"acme-corp"`}</span>,{"\n"}
              {"  image:    "}
              <span style={{ color: "var(--color-amber)" }}>{`"ubuntu-24.04"`}</span>,{"\n"}
              {"  accel:    "}
              <span style={{ color: "var(--color-amber)" }}>{`"h100x8"`}</span>,{"\n"}
              {"  attest:   "}
              <span style={{ color: "#C0C0F2" }}>true</span>,{"\n"}
              {"});"}
            </pre>
          </div>
        </div>
      </div>
      <div style={{ marginTop: "24px" }}>
        <TreatmentSignature
          variant="workshop"
          eyebrow="Email signature · Workshop"
          markVariant="wings-only"
          markAside="Platform · Engineering"
          identity={{
            name: "Engineer Name",
            role: "Platform Engineering · On-call, us-east-1",
          }}
          accent={{ hex: "#F79326", style: "none" }}
          meta={
            <SignatureStatusBadge accentHex="#F79326" onDark>
              incident response · pageable
            </SignatureStatusBadge>
          }
          contact={{ email: "engineer@guardianintelligence.org" }}
        />
      </div>
    </Section>
  );
}

// ============================================================================
// 05 — Treatments · Newsroom
// ============================================================================
function SectionNewsroom() {
  const meta = sectionByID("newsroom");
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
                color: "var(--muted)",
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

      <Surface
        ground="flare"
        style={{ padding: "clamp(32px, 5vw, 72px) clamp(20px, 4vw, 56px)", borderRadius: "16px" }}
      >
        <div style={{ marginBottom: "28px" }}>
          <Lockup size="md" variant="emboss" wordmarkColor="var(--color-ink)" />
        </div>
        <div className="hero-kicker" style={{ color: "rgba(11,11,11,0.7)" }}>
          Seattle · Est. 2026
        </div>
        <h1 className="hero-h1" style={{ color: "var(--color-ink)" }}>
          We build where software meets the real world.
        </h1>
        <p className="hero-lede" style={{ color: "rgba(11,11,11,0.78)" }}>
          Identity, billing, infrastructure, email, and the thousand edges. Open-source per
          subdirectory. Documented end-to-end. Run by the people who built it.
        </p>
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
          <button className="hero-btn ghost" style={{ color: "rgba(11,11,11,0.75)" }}>
            Contact
          </button>
        </div>
      </Surface>
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

// ============================================================================
// 06 — Treatments · Letters
// ============================================================================
function SectionLetters() {
  const meta = sectionByID("letters");
  return (
    <Section
      meta={meta}
      lede={
        <>
          <i>Letters</i> is Guardian's long-form surface, where individual voices show their work.
          Paper ground, Fraunces masthead, Fraunces body for flowing prose, Geist for bylines and
          metadata. <b style={{ color: "var(--color-bordeaux)" }}>Bordeaux</b> marks pull-quotes,
          active links, and drop-cap ornaments — the single editorial accent, reserved for this
          treatment. Flare does not appear on Letters; it is too loud for reading.
        </>
      }
    >
      <TreatmentPalette
        roles={{
          ground: {
            name: "Paper",
            hex: "#F6F4ED",
            note: "Long-form reading canvas.",
            chipStyle: { boxShadow: `inset 0 0 0 1px ${LINE}` },
          },
          accent: {
            name: "Bordeaux",
            hex: "#5C1F1E",
            pantone: "Pantone 504 C",
            note: "Pull-quote rules, drop-caps, active links. Letters only.",
          },
          mark: {
            name: "Argent",
            hex: "#FFFFFF",
            note: "Wings, always — inside the iron chip on paper.",
          },
          muted: {
            name: "Ink",
            hex: "#0B0B0B",
            note: "Bylines, metadata, captions set in Ink @ 0.7 / 0.6 / 0.55 (Stone).",
            chipStyle: { background: "rgba(11,11,11,0.7)" },
          },
        }}
        rule={
          <>
            Bordeaux never ships outside Letters. Flare and Amber never ship <i>into</i> Letters.
            The muted register on Paper is Ink at 0.7 / 0.6 / 0.55 opacity — the warm counterpart to
            Ash on Iron grounds, historically called "Stone".
          </>
        }
      />

      {/* Mark specimen + Type ladder — Letters' "rules" pair. The Paper
          carrier with iron chip sits left; the Fraunces-heavy type ladder
          sits right. The masthead ladder rides full width below — the
          720 px nameplate specimen needs the horizontal room. */}
      <RulesRow>
        <TreatmentMarkCard
          groundVar="var(--color-paper)"
          rows={[
            { label: "Argent · Paper · chip", value: "Letters", emphasise: "name" },
            { label: "ground", value: "#F6F4ED", emphasise: "hex" },
            { label: "chip", value: "#0E0E0E", emphasise: "hex" },
            { label: "wings", value: "#FFFFFF", emphasise: "hex" },
          ]}
        >
          <WingsChip style={{ width: "58%", height: "auto" }} />
        </TreatmentMarkCard>
        {/* Letters is the only treatment where Fraunces sets body prose;
            Geist only handles bylines and metadata. */}
        <TreatmentTypeLadder
          rows={[
            {
              sample: "Applied intelligence is not an adjective.",
              role: "article · h1",
              spec: "Fraunces / 64 / 1.02 / -25 · opsz 144 · SOFT 50",
              sampleSizePx: 48,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 144, "SOFT" 50',
                fontWeight: 400,
                fontSize: "48px",
                lineHeight: 1.02,
                letterSpacing: "-0.025em",
              },
            },
            {
              sample:
                "There is a tradition in the software industry of taking a good word and pointing it at something that has not yet earned it.",
              role: "body prose · fraunces",
              spec: "Fraunces / 19 / 1.55 · opsz 18 · Regular",
              sampleSizePx: 19,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 18, "SOFT" 0',
                fontWeight: 400,
                fontSize: "19px",
                lineHeight: 1.55,
              },
            },
            {
              sample: "— the founder",
              role: "valediction · italic",
              spec: "Fraunces / 22 / italic · opsz 72 · SOFT 60",
              sampleSizePx: 22,
              sampleStyle: {
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 72, "SOFT" 60',
                fontStyle: "italic",
                fontWeight: 400,
                fontSize: "22px",
                lineHeight: 1.3,
                color: "var(--muted-strong)",
              },
            },
            {
              sample: "By the founder · Filed from Seattle, WA",
              role: "byline · meta",
              spec: "Geist / 13 / 1.5 · Stone 0.7",
              sampleSizePx: 13,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 400,
                fontSize: "13px",
                lineHeight: 1.5,
                color: "var(--muted)",
              },
            },
            {
              sample: "Letters № 3 · 19 Apr 2026 · 8 min read",
              role: "eyebrow · upper",
              spec: "Geist / 11 / 1 / +240 · 500 · UPPER",
              sampleSizePx: 11,
              sampleStyle: {
                fontFamily: "'Geist', sans-serif",
                fontWeight: 500,
                fontSize: "11px",
                lineHeight: 1,
                letterSpacing: "0.24em",
                textTransform: "uppercase",
                color: "var(--muted)",
              },
            },
          ]}
          caption={
            <>
              Letters is the only treatment where Fraunces sets body prose. If a surface wants to
              use Fraunces for body outside Letters, it probably wants to be a Letter.
            </>
          }
        />
      </RulesRow>
      <div style={{ marginBottom: "16px" }}>
        {/* Masthead ladder at full width — the 720 px nameplate specimen
            needs the horizontal room to demonstrate the ceiling variant. */}
        <TreatmentMastheadLadder
          rows={[
            { widthPx: 720, issue: "№ 3", date: "19 Apr 2026", label: "ceiling" },
            { widthPx: 480, issue: "№ 3", date: "19 Apr 2026", label: "proportional" },
            { widthPx: 320, issue: "№ 3", date: "19 Apr 2026", label: "floor" },
          ]}
          footer={
            <>
              The nameplate is a volume masthead, not a wordmark lockup. Wings + tracked uppercase
              Geist + Bordeaux rule. The article H1 below it does the heading work; the nameplate
              identifies the publication, not the article.
            </>
          }
        />
      </div>

      <Surface
        ground="paper"
        className="shell-paper"
        style={{ padding: "clamp(32px, 5vw, 64px) clamp(20px, 4vw, 56px)", borderRadius: "16px" }}
      >
        {/* Thin-ruled nameplate — replaces the old size="md" chip Lockup that
            was visually competing with the article H1 below. Wings + tracked
            uppercase Geist "Guardian · Letters" + Bordeaux rule. */}
        <div style={{ margin: "0 0 28px" }}>
          <Nameplate />
        </div>
        {/* Article metadata whispers above the headline. Earlier iterations
            set this at 12 px / 500 with a 24 px flex gap between three spans —
            without explicit separators the three tokens read as one long
            clump ("№ 3  19 APRIL 2026  8 MIN READ"), and the size competed
            with the H1 below for visual weight. 11 px Geist / 0.16 em
            tracking / Stone 0.55 pushes the row below the H1's perceptual
            threshold so the reader's eye starts at the headline, not the
            meta. Middot separators make the tripartite reading semantically
            legible. */}
        <div
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "11px",
            fontWeight: 500,
            letterSpacing: "0.16em",
            textTransform: "uppercase",
            color: "rgba(11,11,11,0.55)",
            margin: "0 0 18px",
            display: "flex",
            gap: "10px",
            alignItems: "center",
          }}
        >
          <span>№&nbsp;3</span>
          <span aria-hidden="true">·</span>
          <span>19 April 2026</span>
          <span aria-hidden="true">·</span>
          <span>8 min read</span>
        </div>
        <h1
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 144, "SOFT" 50, "WONK" 0',
            fontWeight: 400,
            fontSize: "clamp(36px, 6vw, 64px)",
            lineHeight: 1.02,
            letterSpacing: "-0.025em",
            margin: "0 0 20px",
            color: "var(--color-ink)",
            maxWidth: "18ch",
            textTransform: "none",
          }}
        >
          Applied intelligence is not an adjective.
        </h1>
        <div
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "13px",
            color: "#5d5a52",
            margin: "0 0 36px",
            display: "flex",
            gap: "16px",
            alignItems: "center",
          }}
        >
          <span>By the founder</span>
          <span
            style={{ width: "4px", height: "4px", background: "#5d5a52", borderRadius: "2px" }}
          />
          <span>Filed from Seattle, WA</span>
        </div>
        <p
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 18, "SOFT" 0',
            fontWeight: 400,
            fontSize: "19px",
            lineHeight: 1.55,
            color: "var(--color-ink)",
            maxWidth: "58ch",
            margin: "0 0 20px",
          }}
        >
          <span
            aria-hidden="true"
            style={{
              fontFamily: "'Fraunces', Georgia, serif",
              fontVariationSettings: '"opsz" 144, "SOFT" 50',
              fontWeight: 400,
              fontSize: "clamp(56px, 8vw, 88px)",
              lineHeight: 0.9,
              float: "left",
              margin: "6px 14px 0 0",
              color: "var(--color-bordeaux)",
            }}
          >
            T
          </span>
          here is a tradition in the software industry of taking a good word and pointing it at
          something that has not yet earned it. &lsquo;Intelligent&rsquo; dishwashers.{" "}
          &lsquo;Smart&rsquo; calendars. &lsquo;AI-powered&rsquo; spreadsheets. Guardian
          Intelligence is not a linguistic claim. It is a specification. An applied intelligence
          firm ships workloads that run, bills that settle, and companies that scale past their
          founder without hiring a second person.
        </p>
        <blockquote
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontStyle: "italic",
            fontVariationSettings: '"opsz" 72, "SOFT" 60',
            fontWeight: 400,
            fontSize: "clamp(22px, 3.4vw, 34px)",
            lineHeight: 1.2,
            letterSpacing: "-0.012em",
            margin: "32px 0",
            padding: "0 0 0 20px",
            borderLeft: "3px solid var(--color-bordeaux)",
            color: "var(--color-ink)",
            maxWidth: "40ch",
          }}
        >
          &ldquo;A 10,000× increase in value-generation per capita is not a slogan. It is an
          engineering target.&rdquo;
        </blockquote>
        {/* Valediction — this is where the italic "— the founder" belongs,
            inside the article body above the sign-off, not where the author's
            name should be in the signature card. Moved from the old
            LettersSignature. */}
        <p
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 72, "SOFT" 60',
            fontStyle: "italic",
            fontWeight: 400,
            fontSize: "clamp(20px, 2.6vw, 26px)",
            lineHeight: 1.3,
            letterSpacing: "-0.01em",
            margin: "40px 0 0",
            color: "var(--color-ink)",
          }}
        >
          — the founder
        </p>
      </Surface>
      <div style={{ marginTop: "24px" }}>
        {/* The Letters signature drops the "Filed from Seattle, WA · Letter № 3"
            meta row. Both facts already appear in the article body above it —
            the byline carries the city and the eyebrow carries the issue
            number. Restating them on the signature reads as paper-journal
            affectation on an email sign-off; the signature's job is just
            identity + reply route. */}
        <TreatmentSignature
          variant="letters"
          eyebrow="Email signature · Letters"
          markVariant="chip"
          identity={{
            name: "Founder Name",
            role: "Founder · Guardian Intelligence",
          }}
          // Bordeaux rule on the card's left edge — the same editorial gesture
          // the pull-quote above the signature uses. Signature inherits the
          // grammar of the article it accompanies.
          accent={{ hex: "var(--color-bordeaux)", style: "rule-left", heightPx: 3 }}
          contact={{
            email: "letters@guardianintelligence.org",
            secondary: "guardianintelligence.org/letters",
          }}
        />
      </div>
    </Section>
  );
}

// ============================================================================
// 07 — Applied · photography · scrim
// ============================================================================
function SectionPhotography() {
  const meta = sectionByID("photography");
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

function ScrimLabel({ kind, children }: { kind: "no" | "yes"; children: ReactNode }) {
  return (
    <div
      style={{
        position: "absolute",
        top: "14px",
        right: "14px",
        zIndex: 3,
        display: "flex",
        alignItems: "center",
        gap: "8px",
        font: '600 11px/1 "Geist Mono", ui-monospace, monospace',
        fontVariationSettings: '"wght" 600',
        letterSpacing: "0.14em",
        textTransform: "uppercase",
        padding: "7px 10px",
        borderRadius: "999px",
        background: "rgba(14, 14, 14, 0.75)",
        color: "var(--color-type-iron)",
        border: "1px solid rgba(255, 255, 255, 0.10)",
      }}
    >
      <span
        aria-hidden="true"
        style={{
          width: "7px",
          height: "7px",
          borderRadius: "50%",
          display: "inline-block",
          background: kind === "no" ? "#ff5a5a" : "var(--color-flare)",
          boxShadow:
            kind === "no" ? "0 0 0 2px rgba(255,90,90,0.18)" : "0 0 0 2px rgba(204,255,0,0.22)",
        }}
      />
      <span>{children}</span>
    </div>
  );
}

// ============================================================================
// 08 — Applied · business cards
// ============================================================================
function SectionBusinessCards() {
  const meta = sectionByID("business-cards");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Iron default — the working card, carried by everyone. Flare reserved for principals and
          outbound-heavy roles, where the card itself <i>is</i> the action.
        </>
      }
    >
      <div className="grid gap-4 md:grid-cols-2">
        <BizCard ground="iron" />
        <BizCard ground="flare" />
      </div>
    </Section>
  );
}

function BizCard({ ground }: { ground: "iron" | "flare" }) {
  const colors =
    ground === "iron"
      ? { bg: "var(--color-iron)", fg: "var(--color-type-iron)" }
      : { bg: "var(--color-flare)", fg: "var(--color-ink)" };
  return (
    <div
      style={{
        aspectRatio: "3.5 / 2",
        borderRadius: "10px",
        padding: "clamp(14px, 3cqi, 28px)",
        containerType: "inline-size",
        display: "grid",
        gridTemplateRows: "auto 1fr auto",
        gap: "12px",
        border: `1px solid ${LINE}`,
        background: colors.bg,
        color: colors.fg,
      }}
    >
      <Lockup
        size="sm"
        variant={ground === "flare" ? "emboss" : "argent"}
        wordmarkColor={colors.fg}
      />
      <div style={{ alignSelf: "end" }}>
        <div style={{ fontFamily: "'Geist', sans-serif", fontWeight: 600, fontSize: "16px" }}>
          Founder Name
        </div>
        <div
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "12px",
            opacity: 0.65,
            marginTop: "2px",
          }}
        >
          Founder · Applied Intelligence
        </div>
      </div>
      <div
        style={{
          fontFamily: "'Geist Mono', ui-monospace, monospace",
          fontSize: "10px",
          letterSpacing: "0.04em",
          opacity: 0.75,
          display: "flex",
          justifyContent: "space-between",
        }}
      >
        <span>founder@guardianintelligence.org</span>
        <span>+1 (302) XXX XXXX</span>
      </div>
    </div>
  );
}

// ============================================================================
// Shared hero styles — used by Company and Newsroom.
// ============================================================================
const heroStyle = `
  .hero-kicker {
    font: 600 11px/1 "Geist Mono", ui-monospace, monospace;
    font-variation-settings: "wght" 600;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    opacity: 0.72;
    margin-bottom: 16px;
  }
  .hero-h1 {
    font-family: "Fraunces", Georgia, serif;
    font-variation-settings: "opsz" 144, "SOFT" 30;
    font-weight: 400;
    font-size: clamp(38px, 6.8vw, 72px);
    line-height: 1.0;
    letter-spacing: -0.026em;
    margin: 0 0 28px;
    max-width: 22ch;
    text-transform: none;
  }
  .hero-lede {
    font-family: "Geist", sans-serif;
    font-weight: 400;
    font-size: clamp(16px, 2vw, 20px);
    line-height: 1.45;
    max-width: 52ch;
    margin: 0 0 36px;
    opacity: 0.82;
  }
  .hero-cta-row { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
  .mission-block {
    margin-top: 48px;
    padding-top: 40px;
    border-top: 1px solid rgba(245,245,245,0.12);
    max-width: 62ch;
    display: flex;
    flex-direction: column;
    gap: 22px;
  }
  .mission-block p {
    font-family: "Geist", sans-serif;
    font-weight: 400;
    font-size: clamp(16px, 1.7vw, 18px);
    line-height: 1.55;
    margin: 0;
    color: rgba(245,245,245,0.82);
  }
  .mission-block .mission-closer {
    font-family: "Fraunces", Georgia, serif;
    font-variation-settings: "opsz" 72, "SOFT" 30;
    font-weight: 400;
    font-style: italic;
    font-size: clamp(20px, 2.4vw, 26px);
    line-height: 1.3;
    letter-spacing: -0.01em;
    color: var(--color-type-iron);
    max-width: 34ch;
    margin-top: 8px;
  }
  .hero-btn {
    font-family: "Geist", sans-serif;
    font-weight: 500;
    font-size: 14px;
    padding: 12px 20px;
    border-radius: 8px;
    border: 1px solid currentColor;
    background: transparent;
    color: inherit;
    cursor: pointer;
  }
  .hero-btn.primary {
    background: var(--color-flare);
    color: var(--color-ink);
    border-color: var(--color-flare);
  }
  /* Ghost (secondary) actions keep the 1 px hairline on the treatment's
     ink — this is the unified secondary grammar across Company/Workshop/
     Newsroom heros. Earlier the ghost variant set border-color to
     transparent, which collapsed the button into unstyled padded text and
     read as unclickable. The hairline at full opacity makes the control
     obviously interactive while the transparent fill keeps weight below
     the primary. */
  .hero-btn.ghost {
    background: transparent;
    border-color: currentColor;
    opacity: 0.75;
  }
  .hero-btn.ghost:hover { opacity: 1; }
`;

// ============================================================================
// Aggregator — render order matches the nav rail order.
// ============================================================================
export function DesignSections() {
  return (
    <>
      <SectionCompany />
      <SectionWorkshop />
      <SectionNewsroom />
      <SectionLetters />
      <SectionPhotography />
      <SectionBusinessCards />
    </>
  );
}
