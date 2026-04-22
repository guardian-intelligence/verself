import type { CSSProperties, ReactNode } from "react";
import { Lockup, WingsArgent, WingsChip, WingsEmboss } from "@forge-metal/brand";
import { DESIGN_SECTIONS } from "~/lib/design-nav";
import { Section } from "./section-shell";
import {
  SignatureStatusBadge,
  TreatmentLockupLadder,
  TreatmentMarkCard,
  TreatmentPalette,
  TreatmentSignature,
  TreatmentSizeLadder,
  TreatmentTypeLadder,
  TreatmentWingsOnlyLadder,
} from "./treatments";

const sectionByID = (id: (typeof DESIGN_SECTIONS)[number]["id"]) =>
  DESIGN_SECTIONS.find((s) => s.id === id)!;

const PANEL_BG = "#17171a";
const PANEL_2_BG = "#111113";
const LINE = "#2a2a2f";

// ============================================================================
// MarkCard — a panel with a carrier-ground cell on top and meta rows below.
// ============================================================================
function MarkCard({
  ground,
  children,
  rows,
}: {
  readonly ground: string;
  readonly children: ReactNode;
  readonly rows: readonly { label: string; value: string; isName?: boolean; isHex?: boolean }[];
}) {
  return (
    <div
      style={{
        background: PANEL_BG,
        border: `1px solid ${LINE}`,
        borderRadius: "12px",
        overflow: "hidden",
      }}
    >
      <div
        style={{
          background: ground,
          aspectRatio: "1 / 1",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        {children}
      </div>
      <div
        style={{
          font: '600 10px/1.4 "Geist Mono", ui-monospace, monospace',
          fontVariationSettings: '"wght" 600',
          padding: "10px 12px",
          color: "var(--muted)",
          background: PANEL_2_BG,
          borderTop: `1px solid ${LINE}`,
        }}
      >
        {rows.map((row, idx) => (
          <div
            key={idx}
            style={{
              display: "flex",
              justifyContent: "space-between",
              marginTop: idx === 0 ? 0 : "3px",
            }}
          >
            <span
              style={{
                color: row.isName ? "var(--color-type-iron)" : undefined,
                textTransform: row.isName ? "uppercase" : undefined,
                letterSpacing: row.isName ? "0.08em" : undefined,
              }}
            >
              {row.label}
            </span>
            <span
              style={{
                color: row.isHex ? "var(--color-type-iron)" : row.isName ? "var(--muted-faint)" : undefined,
                textTransform: row.isName ? "uppercase" : undefined,
                letterSpacing: row.isName ? "0.08em" : undefined,
              }}
            >
              {row.value}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

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
// TreatmentPalette — every colour the treatment uses, one row, as chips with
// hex / Pantone / role underneath. Lives immediately under each treatment's
// lede so a reader can copy tokens without hunting for them.
// ============================================================================
type Swatch = {
  readonly name: string;
  readonly hex: string;
  readonly role: string;
  readonly pantone?: string;
  readonly chipStyle?: CSSProperties;
};

// Temporary: the legacy palette helper used by treatments still on the old
// layout. Each treatment migration (Workshop → Newsroom → Letters) removes its
// caller; when the last caller is gone the helper goes with it.
function LegacyPalette({
  swatches,
  rule,
}: {
  readonly swatches: readonly Swatch[];
  readonly rule?: ReactNode;
}) {
  return (
    <div
      style={{
        marginBottom: "16px",
        padding: "18px 20px",
        border: `1px solid ${LINE}`,
        borderRadius: "10px",
        background: PANEL_2_BG,
      }}
    >
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: "18px 28px",
          alignItems: "flex-start",
        }}
      >
        {swatches.map((s) => (
          <div key={s.name} style={{ display: "flex", alignItems: "center", gap: "12px" }}>
            <div
              style={{
                width: "40px",
                height: "40px",
                borderRadius: "6px",
                flex: "0 0 40px",
                background: s.hex,
                // Inset 1px hairline so dark chips (Iron, Ink) separate from
                // the dark panel, and light chips (Paper, Argent) get a
                // visible edge. Neutral rgba works on both.
                boxShadow: "inset 0 0 0 1px rgba(128,128,128,0.25)",
                ...s.chipStyle,
              }}
            />
            <div style={{ minWidth: 0 }}>
              <div
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontWeight: 600,
                  fontSize: "13px",
                  color: "var(--color-type-iron)",
                  lineHeight: 1.2,
                }}
              >
                {s.name}
              </div>
              <div
                style={{
                  font: '600 10px/1.35 "Geist Mono", ui-monospace, monospace',
                  fontVariationSettings: '"wght" 600',
                  color: "var(--muted-faint)",
                  letterSpacing: "0.08em",
                  textTransform: "uppercase",
                  marginTop: "2px",
                }}
              >
                {s.hex}
                {s.pantone ? ` · ${s.pantone}` : ""}
              </div>
              <div
                style={{
                  fontFamily: "'Geist', sans-serif",
                  fontSize: "11px",
                  color: "var(--muted)",
                  marginTop: "2px",
                }}
              >
                {s.role}
              </div>
            </div>
          </div>
        ))}
      </div>
      {rule ? (
        <div
          style={{
            marginTop: "14px",
            paddingTop: "14px",
            borderTop: `1px solid ${LINE}`,
            fontFamily: "'Geist', sans-serif",
            fontSize: "12px",
            color: "var(--muted)",
            lineHeight: 1.5,
          }}
        >
          {rule}
        </div>
      ) : null}
    </div>
  );
}

// ============================================================================
// 01 — System · The mark
//
// The mark, end to end: the one-sentence definition, the three carrier
// specimens (Iron / Flare / Paper), the size ladder from favicon to poster,
// and the lockup ladder that governs wordmark pairing at every size. This is
// the only page that answers "what is our mark and how big should it be."
// ============================================================================
function SectionMark() {
  const meta = sectionByID("mark");
  return (
    <Section
      meta={meta}
      lede={
        <>
          The wings are always <b>Argent</b> (#FFFFFF) and never change colour; what changes is the
          treatment they appear in — <b>Company</b> for the corporate record on anveio.com,{" "}
          <b>Workshop</b> for the console and other productivity surfaces,{" "}
          <b>Newsroom</b> for broadcast in acid green, and <b>Letters</b> for individual
          correspondence from engineers, spokespeople, and the founder.
        </>
      }
    >
      <div className="grid gap-4 md:grid-cols-3">
        <MarkCard
          ground="var(--color-iron)"
          rows={[
            { label: "Argent · Iron", value: "Company · Workshop", isName: true },
            { label: "ground", value: "#0E0E0E", isHex: true },
            { label: "wings", value: "#FFFFFF", isHex: true },
          ]}
        >
          <WingsArgent style={{ width: "64%", height: "64%" }} />
        </MarkCard>
        <MarkCard
          ground="var(--color-flare)"
          rows={[
            { label: "Argent · Flare", value: "Newsroom", isName: true },
            { label: "ground", value: "#CCFF00", isHex: true },
            { label: "emboss", value: "#0B0B0B", isHex: true },
            { label: "wings", value: "#FFFFFF", isHex: true },
          ]}
        >
          <WingsEmboss style={{ width: "64%", height: "64%" }} />
        </MarkCard>
        <MarkCard
          ground="var(--color-paper)"
          rows={[
            { label: "Argent · Paper", value: "Letters", isName: true },
            { label: "chip", value: "#0E0E0E", isHex: true },
            { label: "wings", value: "#FFFFFF", isHex: true },
          ]}
        >
          <WingsChip style={{ width: "64%", height: "64%" }} />
        </MarkCard>
      </div>

      {/* Size ladder — the wings hold form from favicon to signage. Below
          16 px the lower wing compacts to a single stroke; favicons carry the
          iron chip so the wings keep their ground regardless of OS chrome. */}
      <div
        style={{
          display: "flex",
          gap: "16px",
          alignItems: "flex-end",
          padding: "32px 24px",
          border: `1px solid ${LINE}`,
          borderRadius: "12px",
          background: PANEL_BG,
          flexWrap: "wrap",
          marginTop: "20px",
        }}
      >
        {[16, 24, 32, 48, 64, 96, 128].map((px) => (
          <div
            key={px}
            style={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              gap: "8px",
            }}
          >
            <WingsChip
              style={{ display: "block", borderRadius: "4px", width: `${px}px`, height: `${px}px` }}
            />
            <small
              style={{
                font: '600 10px/1 "Geist Mono", ui-monospace, monospace',
                fontVariationSettings: '"wght" 600',
                color: "var(--muted-faint)",
                letterSpacing: "0.1em",
              }}
            >
              {px}
            </small>
          </div>
        ))}
      </div>

      {/* Lockup ladder — every wordmark+mark pairing on the page uses the
          Lockup component. Gap is clamp(8, 0.28·mark-h, 18): 8 px floor keeps
          sm legible as a pair, 18 px ceiling keeps lg from flying apart. */}
      <div
        style={{
          marginTop: "20px",
          padding: "28px 32px",
          border: `1px solid ${LINE}`,
          borderRadius: "12px",
          background: "var(--color-iron)",
          display: "grid",
          gap: "24px",
          overflowX: "auto",
        }}
      >
        {[
          { size: "lg" as const, markPx: 96, gap: "18 px", role: "ceiling" },
          { size: "md" as const, markPx: 52, gap: "14.6 px", role: "proportional" },
          { size: "sm" as const, markPx: 28, gap: "8 px", role: "floor" },
        ].map((row) => (
          <div
            key={row.markPx}
            style={{
              display: "grid",
              gridTemplateColumns: "1fr 220px",
              alignItems: "center",
              gap: "12px 24px",
            }}
          >
            <Lockup size={row.size} />
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                gap: "4px",
                font: '600 11px/1.2 "Geist Mono", ui-monospace, monospace',
                fontVariationSettings: '"wght" 600',
                letterSpacing: "0.12em",
                textTransform: "uppercase",
                color: "var(--muted)",
                textAlign: "right",
              }}
            >
              <span>mark {row.markPx} px</span>
              <span>
                gap{" "}
                <b style={{ color: "var(--color-flare)", fontWeight: 600, letterSpacing: "0.08em" }}>
                  {row.gap}
                </b>{" "}
                · {row.role}
              </span>
            </div>
          </div>
        ))}
        <div
          style={{
            borderTop: `1px solid ${LINE}`,
            paddingTop: "16px",
            font: '600 10px/1.4 "Geist Mono", ui-monospace, monospace',
            fontVariationSettings: '"wght" 600',
            letterSpacing: "0.14em",
            textTransform: "uppercase",
            color: "var(--muted-faint)",
          }}
        >
          Clear space · 1 × wing-unit outside the lockup ·{" "}
          <span style={{ color: "var(--muted)" }}>gap clamp(8, 0.28 · mark-h, 18) inside</span>
        </div>
      </div>
    </Section>
  );
}

// ============================================================================
// 02 — System · Typography
// ============================================================================
function SectionTypography() {
  const meta = sectionByID("typography");
  const cell: CSSProperties = {
    padding: "14px 12px",
    borderBottom: `1px solid ${LINE}`,
    textAlign: "left",
    verticalAlign: "middle",
  };
  const headCell: CSSProperties = {
    ...cell,
    font: '600 10px/1 "Geist Mono", ui-monospace, monospace',
    fontVariationSettings: '"wght" 600',
    letterSpacing: "0.12em",
    textTransform: "uppercase",
    color: "var(--muted-faint)",
    paddingBottom: "10px",
  };
  const role: CSSProperties = {
    ...cell,
    color: "var(--muted)",
    fontFamily: "'Geist Mono', ui-monospace, monospace",
    fontSize: "11px",
  };
  const spec: CSSProperties = {
    ...cell,
    color: "var(--muted)",
    fontFamily: "'Geist Mono', ui-monospace, monospace",
    fontSize: "11px",
    whiteSpace: "nowrap",
  };
  const sample: CSSProperties = { ...cell, color: "var(--color-type-iron)" };
  return (
    <Section
      meta={meta}
      lede={
        <>
          <b>Fraunces</b> carries the voice — masthead, headline, editorial. <b>Geist</b> carries
          the work — navigation, controls, data, body, and every surface in Workshop.{" "}
          <b>Geist Mono</b> carries the machine — code, identifiers, telemetry. All three are
          distributed under the SIL Open Font License: free for any use, commercial or otherwise,
          forever.
        </>
      }
    >
      <div style={{ overflowX: "auto" }}>
        <table
          style={{ width: "100%", borderCollapse: "collapse", fontSize: "13px", minWidth: "720px" }}
        >
          <thead>
            <tr>
              <th style={{ ...headCell, width: "55%" }}>Sample</th>
              <th style={headCell}>Role</th>
              <th style={headCell}>Spec</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td
                style={{
                  ...sample,
                  fontFamily: "'Fraunces', Georgia, serif",
                  fontVariationSettings: '"opsz" 144, "SOFT" 30',
                  fontWeight: 400,
                  fontSize: "64px",
                  lineHeight: 1.02,
                  letterSpacing: "-0.025em",
                }}
              >
                The application layer is the product.
              </td>
              <td style={role}>display · hero</td>
              <td style={spec}>Fraunces / 64 / 1.02 / -25 · opsz 144 · SOFT 30</td>
            </tr>
            <tr>
              <td
                style={{
                  ...sample,
                  fontFamily: "'Fraunces', Georgia, serif",
                  fontVariationSettings: '"opsz" 96, "SOFT" 20',
                  fontWeight: 400,
                  fontSize: "48px",
                  lineHeight: 1.05,
                  letterSpacing: "-0.02em",
                }}
              >
                Toward a million solo-founded companies.
              </td>
              <td style={role}>h1 · page</td>
              <td style={spec}>Fraunces / 48 / 1.05 / -20 · opsz 96 · SOFT 20</td>
            </tr>
            <tr>
              <td
                style={{
                  ...sample,
                  fontFamily: "'Fraunces', Georgia, serif",
                  fontVariationSettings: '"opsz" 72',
                  fontWeight: 400,
                  fontSize: "32px",
                  lineHeight: 1.1,
                  letterSpacing: "-0.018em",
                }}
              >
                Compute, integrations, and founder tooling.
              </td>
              <td style={role}>h2 · section</td>
              <td style={spec}>Fraunces / 32 / 1.1 / -18 · opsz 72</td>
            </tr>
            <tr>
              <td
                style={{
                  ...sample,
                  fontFamily: "'Geist', sans-serif",
                  fontWeight: 600,
                  fontSize: "20px",
                  lineHeight: 1.3,
                  letterSpacing: "-0.01em",
                }}
              >
                Sandbox execution
              </td>
              <td style={role}>h3 · ui · workshop</td>
              <td style={spec}>Geist / 20 / 1.3 / -10 · SemiBold</td>
            </tr>
            <tr>
              <td
                style={{
                  ...sample,
                  fontFamily: "'Geist', sans-serif",
                  fontWeight: 400,
                  fontSize: "16px",
                  lineHeight: 1.55,
                }}
              >
                Guardian Intelligence is an American applied intelligence firm. We build the
                compute, the integrations, and the founder tooling that make a one-person
                billion-ARR company an engineering target rather than a slogan.
              </td>
              <td style={role}>body</td>
              <td style={spec}>Geist / 16 / 1.55 · Regular</td>
            </tr>
            <tr>
              <td
                style={{
                  ...sample,
                  fontFamily: "'Geist', sans-serif",
                  fontWeight: 400,
                  fontSize: "13px",
                  lineHeight: 1.5,
                  color: "var(--muted)",
                }}
              >
                Secondary copy, metadata, form help text, caption.
              </td>
              <td style={role}>small · ash</td>
              <td style={spec}>Geist / 13 / 1.5 · Regular · Ash default</td>
            </tr>
            <tr>
              <td
                style={{
                  ...sample,
                  fontFamily: "'Geist Mono', ui-monospace, monospace",
                  fontWeight: 400,
                  fontSize: "12px",
                  lineHeight: 1.5,
                  color: "var(--muted)",
                }}
              >
                curl -sSL guardian.sh | sh
              </td>
              <td style={role}>mono</td>
              <td style={spec}>Geist Mono / 12 / 1.5 · Regular</td>
            </tr>
            <tr>
              <td style={sample}>
                <span
                  style={{
                    fontFamily: "'Geist', sans-serif",
                    fontWeight: 500,
                    fontSize: "10px",
                    lineHeight: 1,
                    letterSpacing: "0.18em",
                    textTransform: "uppercase",
                  }}
                >
                  Letters № 3 · 19 Apr 2026
                </span>
              </td>
              <td style={role}>badge / eyebrow</td>
              <td style={spec}>Geist / 10 / 1 / +180 · Medium · UPPER</td>
            </tr>
          </tbody>
        </table>
      </div>
    </Section>
  );
}

// ============================================================================
// Signature primitives — one per treatment, inlined into the treatment's
// section so the signature reads as part of the same language as the hero.
// ============================================================================

const SIG_CARD: CSSProperties = {
  background: "#fff",
  color: "var(--color-ink)",
  padding: "20px 22px",
  borderRadius: "8px",
  fontFamily: "'Geist', sans-serif",
  fontSize: "13px",
  maxWidth: "540px",
  border: "1px solid #e5e3dc",
};

const SIG_EYEBROW: CSSProperties = {
  font: '600 10px/1 "Geist Mono", ui-monospace, monospace',
  fontVariationSettings: '"wght" 600',
  letterSpacing: "0.16em",
  textTransform: "uppercase",
  color: "var(--muted-faint)",
  marginBottom: "10px",
};

function NewsroomSignature() {
  return (
    <div>
      <div style={SIG_EYEBROW}>Email signature · Newsroom</div>
      <div style={SIG_CARD}>
        <div style={{ marginBottom: "14px" }}>
          <Lockup size="sm" variant="chip" wordmarkColor="var(--color-ink)" />
        </div>
        <div style={{ fontWeight: 600, fontSize: "15px" }}>Press Officer Name</div>
        <div style={{ color: "#5d5a52", marginBottom: "12px" }}>
          Communications · Guardian Intelligence
        </div>
        {/* Flare hairline — the Newsroom's own colour, on the paper canvas an
            external recipient actually sees. */}
        <div
          style={{
            height: "2px",
            width: "44px",
            background: "var(--color-flare)",
            margin: "8px 0 12px",
          }}
        />
        <div style={{ display: "flex", gap: "12px", color: "#5d5a52", fontSize: "12px", flexWrap: "wrap" }}>
          <span>press@guardianintelligence.org</span>
          <span aria-hidden>·</span>
          <span>guardianintelligence.org/press</span>
        </div>
      </div>
    </div>
  );
}

function LettersSignature() {
  return (
    <div>
      <div style={SIG_EYEBROW}>Email signature · Letters</div>
      <div style={{ ...SIG_CARD, background: "var(--color-paper)" }}>
        <div style={{ marginBottom: "14px" }}>
          <Lockup size="sm" variant="chip" wordmarkColor="var(--color-ink)" />
        </div>
        <div
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 72, "SOFT" 30',
            fontStyle: "italic",
            fontSize: "18px",
            lineHeight: 1.3,
            color: "var(--color-ink)",
          }}
        >
          — the founder
        </div>
        <div
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "12px",
            color: "#5d5a52",
            marginTop: "4px",
            marginBottom: "10px",
          }}
        >
          Filed from Seattle, WA · Letter № 3
        </div>
        {/* Bordeaux hairline — the Letters-only accent. */}
        <div
          style={{
            height: "1px",
            width: "44px",
            background: "var(--color-bordeaux)",
            margin: "8px 0 12px",
          }}
        />
        <div style={{ display: "flex", gap: "12px", color: "#5d5a52", fontSize: "12px", flexWrap: "wrap" }}>
          <span>letters@guardianintelligence.org</span>
          <span aria-hidden>·</span>
          <span>guardianintelligence.org/letters</span>
        </div>
      </div>
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

      {/* Mark specimen. Company is the canonical record treatment, so it
          hosts the universal size and lockup ladders that all treatments
          share. The single Iron carrier card here is the Company-specific
          piece; the size ladder shows how wings hold form from favicon to
          signage; the lockup ladder governs every pairing of wings +
          wordmark on the page. */}
      <div style={{ display: "grid", gap: "16px", marginBottom: "16px" }}>
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
              <span style={{ color: "var(--muted)" }}>
                gap clamp(8, 0.28 · mark-h, 18) inside
              </span>
            </>
          }
        />
      </div>

      {/* Type ladder — Company's flavour: the full set from display to badge,
          because Company is where every typographic register of the firm shows
          up (landing → mission → body copy → legal meta). Other treatments
          ship leaner ladders (Workshop drops Fraunces; Newsroom is display-only;
          Letters rebalances Fraunces as the body family). */}
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
            phosphor. <b>Flare is banned from Workshop</b> and <b>Amber never ships outside
            Workshop</b>; the two accents trade places at the chrome boundary so an operator always
            knows which context they are inside.
          </>
        }
      />

      {/* Mark specimen — Iron carrier with wings only (no wordmark ever on
          Workshop surfaces) and a wings-only size ladder culminating at the
          22 px chrome anchor. Workshop has no lockup ladder because Workshop
          never locks up with a wordmark. */}
      <div style={{ display: "grid", gap: "16px", marginBottom: "16px" }}>
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
        <TreatmentWingsOnlyLadder
          note={
            <>
              22 px is the size the live console chrome ships. Below 22 px the glyph starts to
              lose its lower-wing tip at typical display DPI; above 64 px the wings feel like a
              logo looking for a sentence.
            </>
          }
        />
      </div>

      {/* Type ladder — Workshop's flavour: no Fraunces anywhere. H3 is the
          biggest type a console ever sets; body + small + mono carry the rest.
          The spec column notes Geist & Geist Mono against their weights so an
          operator porting styles into a new workshop surface has the recipe
          immediately. */}
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
            sample: "14 active across 4 tenants · 3 h 22 m median lease · 99.98% attestation rate.",
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
                style={{ display: "block", padding: "8px 10px", borderRadius: "6px", color: "var(--muted)" }}
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
                    ["acme-corp", "us-east-1", "inference · h100×8", "0x41e9f2a", "● attested", "ok"],
                    ["hex-labs", "us-east-1", "ci · runner-pool", "0x41e9f2b", "● attested", "ok"],
                    ["lumen-mail", "eu-west-1", "stateful · zfs-pool", "0x41e9f2c", "○ draining", "warn"],
                    ["solo-founder", "us-west-2", "editor · agent-vm", "0x41e9f2d", "● attested", "ok"],
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
          eyebrow="Email signature · Workshop"
          markVariant="wings-only"
          markAside="Platform · Engineering"
          identity={{
            name: "Engineer Name",
            role: "Platform Engineering · On-call, us-east-1",
          }}
          accent={{ hex: "#F79326", style: "none" }}
          meta={
            <SignatureStatusBadge accentHex="#F79326">
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
          <b>OG cards, press hero imagery, and every share preview ride under this treatment by
          default</b> — acid green is how Guardian appears in someone else's feed.
        </>
      }
    >
      <LegacyPalette
        swatches={[
          { name: "Flare", hex: "#CCFF00", pantone: "Pantone 389 C", role: "Ground" },
          { name: "Ink", hex: "#0B0B0B", role: "Type on Flare · ink emboss behind wings" },
          { name: "Argent", hex: "#FFFFFF", role: "Wings (inside circular ink emboss)" },
          { name: "Iron", hex: "#0E0E0E", role: "Inverted primary action · ink-dark button" },
        ]}
        rule={
          <>
            One action, loud and single; everything else reads in ink so the ground does the
            shouting. Bordeaux and Amber never appear here — Newsroom is Flare's surface alone.
          </>
        }
      />
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
        <NewsroomSignature />
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
          metadata.{" "}
          <b style={{ color: "var(--color-bordeaux)" }}>Bordeaux</b> marks pull-quotes, active
          links, and drop-cap ornaments — the single editorial accent, reserved for this treatment.
          Flare does not appear on Letters; it is too loud for reading.
        </>
      }
    >
      <LegacyPalette
        swatches={[
          { name: "Paper", hex: "#F6F4ED", role: "Ground", chipStyle: { boxShadow: `inset 0 0 0 1px ${LINE}` } },
          { name: "Bordeaux", hex: "#5C1F1E", pantone: "Pantone 504 C", role: "Letters-only accent · pull-quote rules, drop-caps, links" },
          { name: "Ink", hex: "#0B0B0B", role: "Body prose · Fraunces" },
          { name: "Stone", hex: "#0B0B0B", role: "Muted ink · bylines, metadata, captions", chipStyle: { background: "rgba(11,11,11,0.7)" } },
          { name: "Argent", hex: "#FFFFFF", role: "Wings (inside iron chip)" },
        ]}
        rule={
          <>
            Bordeaux never ships outside Letters. Flare and Amber never ship <i>into</i> Letters.
            Stone is Paper's muted-ink family — the warm counterpart to Ash on Iron grounds.
          </>
        }
      />
      <Surface
        ground="paper"
        style={{ padding: "clamp(32px, 5vw, 64px) clamp(20px, 4vw, 56px)", borderRadius: "16px" }}
      >
        <div style={{ margin: "0 0 32px" }}>
          <Lockup size="md" variant="chip" wordmark="Guardian · Letters" wordmarkColor="var(--color-ink)" />
        </div>
        <div
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "12px",
            letterSpacing: "0.24em",
            textTransform: "uppercase",
            color: "var(--color-ink)",
            opacity: 0.6,
            margin: "0 0 20px",
            display: "flex",
            gap: "24px",
          }}
        >
          <span>№ 3</span>
          <span>19 April 2026</span>
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
          <span style={{ width: "4px", height: "4px", background: "#5d5a52", borderRadius: "2px" }} />
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
      </Surface>
      <div style={{ marginTop: "24px" }}>
        <LettersSignature />
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
  .hero-btn.ghost { border-color: transparent; opacity: 0.8; }
`;

// ============================================================================
// Aggregator — render order matches the nav rail order.
// ============================================================================
export function DesignSections() {
  return (
    <>
      <SectionMark />
      <SectionTypography />
      <SectionCompany />
      <SectionWorkshop />
      <SectionNewsroom />
      <SectionLetters />
      <SectionPhotography />
      <SectionBusinessCards />
    </>
  );
}
