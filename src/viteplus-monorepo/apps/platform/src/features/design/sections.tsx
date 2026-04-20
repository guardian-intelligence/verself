import type { CSSProperties, ReactNode } from "react";
import {
  Lockup,
  StackedLockup,
  WingsArgent,
  WingsChip,
  WingsEmboss,
} from "@forge-metal/brand";
import { DESIGN_SECTIONS } from "~/lib/design-nav";
import { Section } from "./section-shell";

const sectionByID = (id: (typeof DESIGN_SECTIONS)[number]["id"]) =>
  DESIGN_SECTIONS.find((s) => s.id === id)!;

const PANEL_BG = "#17171a";
const PANEL_2_BG = "#111113";
const LINE = "#2a2a2f";
const MUTED = "rgba(245,245,245,0.6)";
const MUTED_2 = "rgba(245,245,245,0.4)";

// ============================================================================
// Card primitive — matches the playground's `.card { panel + frame + label }`
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
          font: '10px/1.4 "Geist Mono", ui-monospace, monospace',
          padding: "10px 12px",
          color: MUTED,
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
                color: row.isHex
                  ? "var(--color-type-iron)"
                  : row.isName
                    ? MUTED_2
                    : undefined,
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
// 01 — Identity · the mark
// ============================================================================
function SectionMark() {
  const meta = sectionByID("mark");
  return (
    <Section
      meta={meta}
      lede={
        <>
          The upper wing lifts — a swan at takeoff, evoking unprecedented velocity and exponential
          leverage. The lower wing rests — a swan at stillness on water, evoking stability. The
          wings are always <b>Argent</b> (#FFFFFF). On Iron they sit directly; on Paper they sit
          inside a rounded iron chip; on Flare they sit inside a circular ink emboss. The shape of
          the carrier is semantic — the square chip is editorial, the circular emboss is broadcast.
        </>
      }
    >
      <div className="grid gap-4 md:grid-cols-3">
        <MarkCard
          ground="var(--color-iron)"
          rows={[
            { label: "Argent · Iron", value: "customers", isName: true },
            { label: "ground", value: "#0E0E0E", isHex: true },
            { label: "wings", value: "#FFFFFF", isHex: true },
          ]}
        >
          <WingsArgent style={{ width: "64%", height: "64%" }} />
        </MarkCard>
        <MarkCard
          ground="var(--color-flare)"
          rows={[
            { label: "Argent · Flare", value: "the world", isName: true },
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
            { label: "Argent · Paper", value: "editorial", isName: true },
            { label: "chip", value: "#0E0E0E", isHex: true },
            { label: "wings", value: "#FFFFFF", isHex: true },
          ]}
        >
          <WingsChip style={{ width: "64%", height: "64%" }} />
        </MarkCard>
      </div>
    </Section>
  );
}

// ============================================================================
// 02 — Identity · audience split
// ============================================================================
function SectionAudienceSplit() {
  const meta = sectionByID("audience");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Argent on Iron is the mark of the work — customers, product, docs, billing, contracts. It
          does not compete for attention; it earns trust by showing up the same way every time.
          Argent on Flare — wings in a circular ink emboss — is the mark of broadcast: social,
          press, investor covers, recruiting, signage. It is the moment the brand wants to be
          noticed. A single surface uses one treatment, not both.
        </>
      }
    >
      <div className="grid gap-4 md:grid-cols-2">
        <AudiencePanel ground="iron">
          <div className="role-eyebrow">Customers</div>
          <Lockup size="lg" />
          <p className="audience-job">
            Product UI, docs, dashboards, billing, contracts, email. Where the work happens.
          </p>
        </AudiencePanel>
        <AudiencePanel ground="flare">
          <div className="role-eyebrow" style={{ color: "rgba(11,11,11,0.7)" }}>
            The world
          </div>
          <Lockup size="lg" variant="emboss" wordmarkColor="var(--color-ink)" />
          <p className="audience-job" style={{ color: "var(--color-ink)" }}>
            Social, press, investor decks, billboards, conferences, merch. Where attention is
            captured.
          </p>
        </AudiencePanel>
      </div>
      <style>{audiencePanelStyle}</style>
    </Section>
  );
}

const audiencePanelStyle = `
  .audience-panel {
    border-radius: 12px;
    border: 1px solid ${LINE};
    overflow: hidden;
    min-height: 260px;
    padding: 40px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    gap: 24px;
  }
  .audience-panel.iron { background: var(--color-iron); color: var(--color-type-iron); }
  .audience-panel.flare { background: var(--color-flare); color: var(--color-ink); }
  .audience-panel .role-eyebrow {
    font: 500 11px/1 "Geist Mono", ui-monospace, monospace;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    opacity: 0.7;
  }
  .audience-panel .audience-job {
    font-family: "Fraunces", Georgia, serif;
    font-variation-settings: "opsz" 96, "SOFT" 20;
    font-weight: 400;
    font-size: 28px;
    line-height: 1.15;
    letter-spacing: -0.015em;
    margin: 0;
  }
`;

function AudiencePanel({
  ground,
  children,
}: {
  readonly ground: "iron" | "flare";
  readonly children: ReactNode;
}) {
  return (
    <div
      style={{
        borderRadius: "12px",
        border: `1px solid ${LINE}`,
        overflow: "hidden",
      }}
    >
      <div className={`audience-panel ${ground}`}>{children}</div>
    </div>
  );
}

// ============================================================================
// 03 — Identity · clear space
// ============================================================================
function SectionClearSpace() {
  const meta = sectionByID("clear-space");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Two rules, one measurement. <b>Outside</b> the lockup, clear space equals the height of
          the upper wing's tip, exposed as{" "}
          <code style={{ color: "var(--color-type-iron)" }}>--wing-unit</code>. <b>Inside</b> the
          lockup, the gap between mark and wordmark uses{" "}
          <code style={{ color: "var(--color-type-iron)" }}>
            clamp(8px, 0.28·mark-h, 18px)
          </code>{" "}
          — proportional most of the time, but with a floor so small surfaces still read as a
          lockup and a ceiling so oversize ones don't feel airy. Below the floor, the mark and
          wordmark stop reading as paired; above the ceiling, the mark stops looking like it
          belongs to the wordmark.
        </>
      }
    >
      <div
        style={{
          position: "relative",
          background: `
            linear-gradient(to right, rgba(204,255,0,0.18) 1px, transparent 1px) 0 0 / calc(64px * 0.45) 100%,
            linear-gradient(to bottom, rgba(204,255,0,0.18) 1px, transparent 1px) 0 0 / 100% calc(64px * 0.45),
            var(--color-iron)
          `,
          padding: "calc(64px * 0.45)",
          border: `1px solid ${LINE}`,
          borderRadius: "12px",
        }}
      >
        <span
          style={{
            display: "inline-block",
            outline: "1px dashed rgba(204,255,0,0.55)",
            outlineOffset: "calc(64px * 0.45)",
            padding: "8px 12px",
          }}
        >
          <Lockup size="md" />
        </span>
        <small
          style={{
            display: "block",
            marginTop: "calc(64px * 0.54)",
            color: MUTED,
            font: '500 11px/1 "Geist Mono", ui-monospace, monospace',
            letterSpacing: "0.12em",
            textTransform: "uppercase",
          }}
        >
          Dashed outline · 1× wing-unit · internal gap · clamp(8, 0.28·mark-h, 18)
        </small>
      </div>

      <div
        style={{
          marginTop: "20px",
          padding: "24px 28px",
          border: `1px solid ${LINE}`,
          borderRadius: "12px",
          background: PANEL_2_BG,
          display: "grid",
          gap: "24px",
        }}
      >
        {[
          { size: "lg", markPx: 96, gap: "18 px", role: "ceiling" },
          { size: "md", markPx: 52, gap: "14.6 px", role: "proportional" },
          { size: "sm", markPx: 28, gap: "8 px", role: "floor" },
        ].map((row) => (
          <div
            key={row.markPx}
            style={{
              display: "grid",
              gridTemplateColumns: "1fr 220px",
              alignItems: "center",
              gap: "24px",
            }}
          >
            <Lockup size={row.size as "sm" | "md" | "lg"} />
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                gap: "4px",
                font: '500 11px/1.2 "Geist Mono", ui-monospace, monospace',
                letterSpacing: "0.12em",
                textTransform: "uppercase",
                color: MUTED,
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
      </div>
    </Section>
  );
}

// ============================================================================
// 04 — Identity · size ladder
// ============================================================================
function SectionSizeLadder() {
  const meta = sectionByID("size-ladder");
  return (
    <Section
      meta={meta}
      lede={
        <>
          The wings hold form from 16 px to 512 px. Below 16 px, the lower wing compacts to a
          single stroke — a silhouette, not an illustration. Favicons and app-icons always carry
          the iron chip, so the wings keep their ground regardless of where the operating system
          drops them.
        </>
      }
    >
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
                font: '500 10px/1 "Geist Mono", ui-monospace, monospace',
                color: MUTED_2,
                letterSpacing: "0.1em",
              }}
            >
              {px}
            </small>
          </div>
        ))}
      </div>
    </Section>
  );
}

// ============================================================================
// 05 — Identity · lockups
// ============================================================================
function SectionLockups() {
  const meta = sectionByID("lockups");
  const eyebrowStyle: CSSProperties = {
    fontFamily: "'Geist Mono', ui-monospace, monospace",
    fontSize: "12px",
    color: MUTED,
    opacity: 0.55,
    marginBottom: "20px",
  };
  return (
    <Section
      meta={meta}
      lede={
        <>
          <i>Guardian Intelligence</i> sets in <b>Fraunces</b> at display scale — a serif masthead,
          not a technology wordmark. The short form &ldquo;Guardian&rdquo; is for second-reference
          uses: favicons, signatures, inline mentions. The gap between mark and wordmark is one
          quarter of the mark's height; this never changes.
        </>
      }
    >
      <Surface ground="iron">
        <div style={eyebrowStyle}>HORIZONTAL · LARGE</div>
        <Lockup size="lg" />
        <div style={{ ...eyebrowStyle, margin: "40px 0 20px" }}>HORIZONTAL · DEFAULT</div>
        <Lockup size="md" />
        <div style={{ ...eyebrowStyle, margin: "40px 0 20px" }}>
          HORIZONTAL · SMALL · SHORT FORM
        </div>
        <Lockup size="sm" wordmark="Guardian" />
        <div style={{ ...eyebrowStyle, margin: "40px 0 0" }}>
          STACKED · CENTRED · WITH TAGLINE
        </div>
        <div style={{ display: "flex", justifyContent: "center" }}>
          <StackedLockup tagline="American Applied Intelligence" />
        </div>
      </Surface>
    </Section>
  );
}

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
// 06 — System · colour
// ============================================================================
function SectionColour() {
  const meta = sectionByID("colour");
  return (
    <Section
      meta={meta}
      lede={
        <>
          <b>Iron</b> is the stage — the default canvas for everything the company actually ships.{" "}
          <b>Flare</b> is the action — Pantone 389 C — used sparingly, 99% of the time reserved for
          the single primary action in view. <b>Paper</b> is the editorial ground, for long-form
          prose. Two accents travel between them: <b>Argent</b> is the wings' colour — never a
          ground — and <b>Oxblood</b> is the editorial mark, appearing only on Paper to rule
          pull-quotes and underline the links worth following.
        </>
      }
    >
      <div className="grid gap-3" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))" }}>
        {[
          {
            n: "Iron",
            d: "Primary dark ground · the default stage",
            k: "HEX #0E0E0E · RGB 14 · 14 · 14",
            chip: "var(--color-iron)",
          },
          {
            n: "Flare · Pantone 389 C",
            d: "Broadcast ground · primary action",
            k: "HEX #CCFF00 · RGB 204 · 255 · 0 · CMYK 25 · 0 · 100 · 0",
            chip: "var(--color-flare)",
          },
          {
            n: "Paper",
            d: "Editorial ground · long-form, print",
            k: "HEX #F6F4ED · RGB 246 · 244 · 237",
            chip: "var(--color-paper)",
            chipBorder: true,
          },
          {
            n: "Argent",
            d: "The wings. Never a ground.",
            k: "HEX #FFFFFF · RGB 255 · 255 · 255",
            chip: "#FFFFFF",
          },
          {
            n: "Oxblood",
            d: "Editorial accent. Paper-only. Pull-quote rules, active links, drop-cap ornaments.",
            k: "HEX #5C1F1E · RGB 92 · 31 · 30",
            chip: "var(--color-oxblood)",
          },
        ].map((s) => (
          <div
            key={s.n}
            style={{
              borderRadius: "12px",
              overflow: "hidden",
              border: `1px solid ${LINE}`,
            }}
          >
            <div
              style={{
                background: s.chip,
                height: "110px",
                ...(s.chipBorder ? { boxShadow: `inset 0 0 0 1px ${LINE}` } : {}),
              }}
            />
            <div style={{ padding: "14px", background: PANEL_BG }}>
              <div
                style={{
                  fontWeight: 600,
                  fontSize: "14px",
                  color: "var(--color-type-iron)",
                  fontFamily: "'Geist', sans-serif",
                }}
              >
                {s.n}
              </div>
              <div
                style={{
                  color: MUTED,
                  fontSize: "12px",
                  marginTop: "2px",
                  fontFamily: "'Geist', sans-serif",
                }}
              >
                {s.d}
              </div>
              <div
                style={{
                  font: '500 10px/1.3 "Geist Mono", ui-monospace, monospace',
                  color: MUTED_2,
                  letterSpacing: "0.08em",
                  textTransform: "uppercase",
                  marginTop: "6px",
                }}
              >
                {s.k}
              </div>
            </div>
          </div>
        ))}
      </div>
    </Section>
  );
}

// ============================================================================
// 07 — System · typography
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
    font: '500 10px/1 "Geist Mono", ui-monospace, monospace',
    letterSpacing: "0.12em",
    textTransform: "uppercase",
    color: MUTED_2,
    paddingBottom: "10px",
  };
  const role: CSSProperties = {
    ...cell,
    color: MUTED,
    fontFamily: "'Geist Mono', ui-monospace, monospace",
    fontSize: "11px",
  };
  const spec: CSSProperties = {
    ...cell,
    color: MUTED,
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
          the work — navigation, controls, data, body. <b>Geist Mono</b> carries the machine —
          code, identifiers, telemetry. All three are distributed under the SIL Open Font License:
          free for any use, commercial or otherwise, forever. No vendor blockers, no per-seat
          licence, no renewal risk.
        </>
      }
    >
      <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "13px" }}>
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
              Compute, integrations, and operator tooling.
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
            <td style={role}>h3 · ui</td>
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
              Guardian Intelligence is an American applied intelligence firm. We build the compute,
              the integrations, and the operator tooling that make a one-person billion-ARR company
              an engineering target rather than a slogan.
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
                color: MUTED,
              }}
            >
              Secondary copy, metadata, form help text, caption.
            </td>
            <td style={role}>small</td>
            <td style={spec}>Geist / 13 / 1.5 · Regular</td>
          </tr>
          <tr>
            <td
              style={{
                ...sample,
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                fontWeight: 400,
                fontSize: "12px",
                lineHeight: 1.5,
                color: MUTED,
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
                Dispatch № 3 · 19 Apr 2026
              </span>
            </td>
            <td style={role}>badge / eyebrow</td>
            <td style={spec}>Geist / 10 / 1 / +180 · Medium · UPPER</td>
          </tr>
        </tbody>
      </table>
    </Section>
  );
}

// ============================================================================
// 08 — Applied · hero · Iron
// ============================================================================
function SectionHeroIron() {
  const meta = sectionByID("hero-iron");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Argent on Iron. The work mark. The page the customer signs up on, the page they sign in
          to, the page they read the docs on.
        </>
      }
    >
      <Surface ground="iron" style={{ padding: "72px 56px", borderRadius: "16px" }}>
        <div className="hero-kicker">
          An American Applied Intelligence firm · Est. 2026 · Delaware
        </div>
        <h1 className="hero-h1">Toward the first million solo-founded billion-ARR startups.</h1>
        <p className="hero-lede">
          We begin with the application layer for AI — compute services, enterprise integrations,
          and operator tooling. A 10,000× increase in value-generation per capita is not a slogan.
          It is an engineering target. One person proves it can be done. Then we open source the
          formula for everyone with an idea.
        </p>
        <div className="hero-cta-row">
          <button className="hero-btn primary">Request access</button>
          <button className="hero-btn ghost">Read the dispatch →</button>
        </div>
      </Surface>
      <style>{heroStyle}</style>
    </Section>
  );
}

// ============================================================================
// 09 — Applied · hero · Flare
// ============================================================================
function SectionHeroFlare() {
  const meta = sectionByID("hero-flare");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Argent on Flare, carried in a circular ink emboss. The broadcast mark. Investor deck
          covers, billboards, social hero images, recruiting posters, conference backdrops, merch.
        </>
      }
    >
      <Surface ground="flare" style={{ padding: "72px 56px", borderRadius: "16px" }}>
        <div style={{ marginBottom: "20px" }}>
          <Lockup size="md" variant="emboss" wordmarkColor="var(--color-ink)" />
        </div>
        <div className="hero-kicker" style={{ color: "rgba(11,11,11,0.7)" }}>
          Applied since day one
        </div>
        <h1 className="hero-h1" style={{ color: "var(--color-ink)" }}>
          A company is the smallest possible unit of applied intelligence.
        </h1>
        <p className="hero-lede" style={{ color: "rgba(11,11,11,0.78)" }}>
          When one person can run a billion-dollar company, the scarcity in the economy is no
          longer labour. It is judgment. We design the tools that make judgment compound.
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
            Join the waitlist
          </button>
          <button className="hero-btn ghost" style={{ color: "rgba(11,11,11,0.75)" }}>
            Contact
          </button>
        </div>
      </Surface>
    </Section>
  );
}

const heroStyle = `
  .hero-kicker {
    font: 500 11px/1 "Geist Mono", ui-monospace, monospace;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    opacity: 0.7;
    margin-bottom: 16px;
  }
  .hero-h1 {
    font-family: "Fraunces", Georgia, serif;
    font-variation-settings: "opsz" 144, "SOFT" 30;
    font-weight: 400;
    font-size: 84px;
    line-height: 0.98;
    letter-spacing: -0.028em;
    margin: 0 0 24px;
    max-width: 16ch;
    text-transform: none;
  }
  .hero-lede {
    font-family: "Geist", sans-serif;
    font-weight: 400;
    font-size: 20px;
    line-height: 1.45;
    max-width: 52ch;
    margin: 0 0 36px;
    opacity: 0.82;
  }
  .hero-cta-row { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
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
// 10 — Applied · Dispatch
// ============================================================================
function SectionDispatch() {
  const meta = sectionByID("dispatch");
  return (
    <Section
      meta={meta}
      lede={
        <>
          The <i>Dispatch</i> — Guardian's essay surface. Paper ground, Fraunces masthead, Fraunces
          body for flowing prose, Geist for bylines and metadata. The mark travels to Paper inside
          its iron chip — the wings never change colour.{" "}
          <b style={{ color: "var(--color-oxblood)" }}>Oxblood</b> (#5C1F1E) marks pull-quotes,
          active links, and drop-cap ornaments — the one editorial-only accent, reserved for Paper
          surfaces. Flare does not appear on editorial; it is too loud for reading.
        </>
      }
    >
      <Surface ground="paper" style={{ padding: "64px 56px", borderRadius: "16px" }}>
        <div style={{ display: "flex", alignItems: "center", gap: "16px", margin: "0 0 32px" }}>
          <WingsChip style={{ width: "36px", height: "36px" }} />
          <span
            style={{
              fontFamily: "'Fraunces', Georgia, serif",
              fontVariationSettings: '"opsz" 72',
              fontSize: "22px",
              letterSpacing: "-0.01em",
              color: "var(--color-ink)",
            }}
          >
            Guardian · Dispatch
          </span>
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
            fontSize: "64px",
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
          <span>By the operator</span>
          <span
            style={{ width: "4px", height: "4px", background: "#5d5a52", borderRadius: "2px" }}
          />
          <span>Filed from Wilmington, DE</span>
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
              fontSize: "88px",
              lineHeight: 0.9,
              float: "left",
              margin: "6px 14px 0 0",
              color: "var(--color-oxblood)",
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
            fontSize: "34px",
            lineHeight: 1.2,
            letterSpacing: "-0.012em",
            margin: "32px 0",
            padding: "0 0 0 20px",
            borderLeft: "3px solid var(--color-oxblood)",
            color: "var(--color-ink)",
            maxWidth: "40ch",
          }}
        >
          &ldquo;A 10,000× increase in value-generation per capita is not a slogan. It is an
          engineering target.&rdquo;
        </blockquote>
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
          The argument has three parts. The first is compute: that a single operator with the right
          infrastructure can stand up, scale, and shut down services that previously required a
          platform team. The second is integration: that the economic work of a company is mostly
          the work of moving structured information between counterparties, and that most of this
          work is mechanically obvious once named. The third is tooling for the operator themselves
          — the judgment-amplification layer, which is the hardest to build and the easiest to
          recognise once you've used one that works.
        </p>
      </Surface>
    </Section>
  );
}

// ============================================================================
// 11 — Applied · product chrome
// ============================================================================
function SectionProduct() {
  const meta = sectionByID("product");
  return (
    <Section
      meta={meta}
      lede={
        <>
          Inside the product, the wordmark remains in Fraunces. Everything else — navigation,
          controls, data, code — sets in Geist and Geist Mono. The mark is Argent on Iron, direct —
          no chip, the canvas is already the wings' ground.
        </>
      }
    >
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
          <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
            <WingsArgent style={{ width: "22px", height: "22px" }} />
            <span
              style={{
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 72',
                fontSize: "18px",
                letterSpacing: "-0.01em",
              }}
            >
              Guardian
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
            <span style={{ color: MUTED }}>Compute</span>
            <span style={{ color: MUTED }}>Integrations</span>
            <span style={{ color: MUTED }}>Operators</span>
            <span style={{ color: MUTED }}>Dispatches</span>
          </nav>
          <div style={{ display: "flex", gap: "10px", alignItems: "center" }}>
            <span
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: "6px",
                fontSize: "10px",
                letterSpacing: "0.16em",
                textTransform: "uppercase",
                padding: "4px 10px",
                borderRadius: "999px",
                border: "1px solid var(--color-flare)",
                fontFamily: "'Geist', sans-serif",
                fontWeight: 500,
                color: "var(--color-flare)",
              }}
            >
              ● Live
            </span>
            <button
              className="hero-btn primary"
              style={{ padding: "8px 14px", fontSize: "13px" }}
            >
              Deploy
            </button>
          </div>
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "220px 1fr", minHeight: "420px" }}>
          <aside
            style={{
              borderRight: `1px solid ${LINE}`,
              padding: "20px 16px",
              fontFamily: "'Geist', sans-serif",
              fontSize: "13px",
            }}
          >
            <div
              style={{
                color: MUTED_2,
                fontSize: "10px",
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
                  color: item.active ? "var(--color-type-iron)" : MUTED,
                  background: item.active ? "#1c1c20" : "transparent",
                }}
              >
                {item.label}
              </span>
            ))}
            <div
              style={{
                color: MUTED_2,
                fontSize: "10px",
                letterSpacing: "0.16em",
                textTransform: "uppercase",
                margin: "20px 8px 8px",
              }}
            >
              Operator
            </div>
            {["Dispatches", "Integrations", "Billing"].map((label) => (
              <span
                key={label}
                style={{ display: "block", padding: "8px 10px", borderRadius: "6px", color: MUTED }}
              >
                {label}
              </span>
            ))}
          </aside>
          <div style={{ padding: "28px 32px" }}>
            <h2
              style={{
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 96',
                fontWeight: 400,
                fontSize: "32px",
                letterSpacing: "-0.018em",
                margin: "0 0 6px",
                color: "var(--color-type-iron)",
                textTransform: "none",
              }}
            >
              Production sandboxes
            </h2>
            <p
              style={{
                color: MUTED,
                fontFamily: "'Geist', sans-serif",
                fontSize: "14px",
                margin: "0 0 20px",
              }}
            >
              14 active across 4 tenants · 3 h 22 m median lease · 99.98% attestation rate
            </p>
            <table
              style={{
                width: "100%",
                borderCollapse: "collapse",
                fontFamily: "'Geist', sans-serif",
                fontSize: "13px",
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
                        letterSpacing: "0.14em",
                        textTransform: "uppercase",
                        color: MUTED_2,
                        fontWeight: 500,
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
                  [
                    "lumen-mail",
                    "eu-west-1",
                    "stateful · zfs-pool",
                    "0x41e9f2c",
                    "○ draining",
                    "warn",
                  ],
                  [
                    "solo-operator",
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
                        color: row[5] === "ok" ? "var(--color-flare)" : "#f0c74f",
                        fontWeight: 500,
                      }}
                    >
                      {row[4]}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
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
                {"// Deploy a Guardian sandbox from the operator CLI."}
              </span>
              {"\n"}
              <span style={{ color: "#C0C0F2" }}>import</span>
              {" { sandbox } "}
              <span style={{ color: "#C0C0F2" }}>from</span>{" "}
              <span style={{ color: "var(--color-flare)" }}>{`"@guardian/compute"`}</span>;{"\n\n"}
              <span style={{ color: "#C0C0F2" }}>await</span> sandbox.run({"{"}
              {"\n"}
              {"  tenant:   "}
              <span style={{ color: "var(--color-flare)" }}>{`"acme-corp"`}</span>,{"\n"}
              {"  image:    "}
              <span style={{ color: "var(--color-flare)" }}>{`"ubuntu-24.04"`}</span>,{"\n"}
              {"  accel:    "}
              <span style={{ color: "var(--color-flare)" }}>{`"h100x8"`}</span>,{"\n"}
              {"  attest:   "}
              <span style={{ color: "#C0C0F2" }}>true</span>,{"\n"}
              {"});"}
            </pre>
          </div>
        </div>
      </div>
    </Section>
  );
}

// ============================================================================
// 12 — Applied · photography · scrim
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
    display: "flex",
    alignItems: "center",
    gap: "calc(64px * 0.405)",
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
          saw. Used on keynote slides, investor deck covers, hero posters, recruiting imagery,
          trade-show backdrops.
        </>
      }
    >
      <div className="grid gap-4 md:grid-cols-2">
        <div style={card}>
          <div style={ground} />
          <div style={photoMark}>
            <WingsArgent style={{ width: "44px", height: "44px" }} />
            <span
              style={{
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 96, "SOFT" 30',
                color: "var(--color-argent)",
                fontSize: "24px",
                letterSpacing: "-0.01em",
              }}
            >
              Guardian Intelligence
            </span>
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
            <WingsArgent style={{ width: "44px", height: "44px" }} />
            <span
              style={{
                fontFamily: "'Fraunces', Georgia, serif",
                fontVariationSettings: '"opsz" 96, "SOFT" 30',
                color: "var(--color-argent)",
                fontSize: "24px",
                letterSpacing: "-0.01em",
              }}
            >
              Guardian Intelligence
            </span>
          </div>
          <ScrimLabel kind="yes">With scrim · 3:1 floor</ScrimLabel>
        </div>
      </div>
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "140px 1fr",
          gap: "6px 24px",
          marginTop: "16px",
          padding: "20px 22px",
          border: `1px solid ${LINE}`,
          borderRadius: "10px",
          background: PANEL_BG,
          fontFamily: "'Geist', sans-serif",
          fontSize: "13px",
          color: "var(--color-type-iron)",
        }}
      >
        {[
          ["colour", "Iron · #0E0E0E"],
          ["gradient", "180° · 0% → 45% (0.20 α) → 90% (0.75 α) → 100% (0.90 α)"],
          ["blur", "32 – 48 px Gaussian, optional"],
          ["mark position", "bottom-anchored · left: var(--wing-unit) · bottom: var(--wing-unit)"],
          ["contrast floor", "3:1 WCAG against mark centroid · measured post-scrim"],
        ].map(([k, v]) => (
          <div key={k} style={{ display: "contents" }}>
            <span
              style={{
                color: MUTED,
                fontFamily: "'Geist Mono', ui-monospace, monospace",
                fontSize: "11px",
                letterSpacing: "0.12em",
                textTransform: "uppercase",
                paddingTop: "4px",
              }}
            >
              {k}
            </span>
            <span style={{ paddingTop: "4px" }}>{v}</span>
          </div>
        ))}
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
        font: '500 11px/1 "Geist Mono", ui-monospace, monospace',
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
// 13 — Applied · OG card
// ============================================================================
function SectionOgCard() {
  const meta = sectionByID("og-card");
  return (
    <Section
      meta={meta}
      lede={
        <>
          What Guardian Intelligence looks like when it appears in someone else's feed. Iron
          canvas; Argent mark; Flare only on the one word that earns it.
        </>
      }
    >
      <div
        style={{
          width: "100%",
          aspectRatio: "1200 / 630",
          background: "var(--color-iron)",
          color: "var(--color-type-iron)",
          borderRadius: "12px",
          border: `1px solid ${LINE}`,
          padding: "56px",
          position: "relative",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: "14px" }}>
          <WingsArgent style={{ width: "44px", height: "44px" }} />
          <span
            style={{
              fontFamily: "'Fraunces', Georgia, serif",
              fontVariationSettings: '"opsz" 72',
              fontSize: "28px",
              letterSpacing: "-0.01em",
            }}
          >
            Guardian Intelligence
          </span>
        </div>
        <div
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 144, "SOFT" 40',
            fontWeight: 400,
            fontSize: "56px",
            lineHeight: 1.02,
            letterSpacing: "-0.025em",
            maxWidth: "22ch",
          }}
        >
          Toward the first million solo-founded{" "}
          <span style={{ color: "var(--color-flare)" }}>billion-ARR</span> startups.
        </div>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            fontFamily: "'Geist', sans-serif",
            fontSize: "13px",
            color: MUTED,
          }}
        >
          <span>guardian.com</span>
          <span style={{ color: "var(--color-flare)" }}>Applied, not promised.</span>
        </div>
      </div>
    </Section>
  );
}

// ============================================================================
// 14 — Applied · business cards
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
        padding: "28px",
        display: "grid",
        gridTemplateRows: "auto 1fr auto",
        gap: "12px",
        border: `1px solid ${LINE}`,
        background: colors.bg,
        color: colors.fg,
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
        {ground === "iron" ? (
          <WingsArgent style={{ width: "28px", height: "28px", flex: "0 0 28px" }} />
        ) : (
          <WingsEmboss style={{ width: "28px", height: "28px", flex: "0 0 28px" }} />
        )}
        <span
          style={{
            fontFamily: "'Fraunces', Georgia, serif",
            fontVariationSettings: '"opsz" 72',
            fontSize: "22px",
            letterSpacing: "-0.01em",
          }}
        >
          Guardian Intelligence
        </span>
      </div>
      <div style={{ alignSelf: "end" }}>
        <div style={{ fontFamily: "'Geist', sans-serif", fontWeight: 600, fontSize: "16px" }}>
          Operator Name
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
        <span>operator@guardian.com</span>
        <span>+1 (302) XXX XXXX</span>
      </div>
    </div>
  );
}

// ============================================================================
// 15 — Applied · email signature
// ============================================================================
function SectionEmailSignature() {
  const meta = sectionByID("email-signature");
  return (
    <Section
      meta={meta}
      lede={
        <>
          The recipient's client draws the canvas — usually white, sometimes paper. The iron chip
          carries the Argent wings through whatever ground shows up. Renders in Gmail, Outlook,
          Apple Mail. The Fraunces wordmark is SVG; body falls back to system sans.
        </>
      }
    >
      <div
        style={{
          background: "#fff",
          color: "var(--color-ink)",
          padding: "20px 22px",
          borderRadius: "8px",
          fontFamily: "'Geist', sans-serif",
          fontSize: "13px",
          maxWidth: "540px",
          border: "1px solid #e5e3dc",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: "12px",
            marginBottom: "14px",
          }}
        >
          <WingsChip style={{ width: "28px", height: "28px", flex: "0 0 28px" }} />
          <span
            style={{
              fontFamily: "'Fraunces', Georgia, serif",
              fontVariationSettings: '"opsz" 72',
              fontSize: "18px",
              letterSpacing: "-0.01em",
              color: "var(--color-ink)",
            }}
          >
            Guardian Intelligence
          </span>
        </div>
        <div style={{ fontWeight: 600, fontSize: "15px" }}>Operator Name</div>
        <div style={{ color: "#5d5a52", marginBottom: "12px" }}>Founder · Applied Intelligence</div>
        <div
          style={{
            height: "2px",
            width: "44px",
            background: "var(--color-flare)",
            margin: "8px 0 12px",
          }}
        />
        <div style={{ display: "flex", gap: "12px", color: "#5d5a52", fontSize: "12px" }}>
          <span>operator@guardian.com</span>
          <span>·</span>
          <span>guardian.com</span>
          <span>·</span>
          <span>/dispatch</span>
        </div>
      </div>
    </Section>
  );
}

// ============================================================================
// Aggregator
// ============================================================================
export function DesignSections() {
  return (
    <>
      <SectionMark />
      <SectionAudienceSplit />
      <SectionClearSpace />
      <SectionSizeLadder />
      <SectionLockups />
      <SectionColour />
      <SectionTypography />
      <SectionHeroIron />
      <SectionHeroFlare />
      <SectionDispatch />
      <SectionProduct />
      <SectionPhotography />
      <SectionOgCard />
      <SectionBusinessCards />
      <SectionEmailSignature />
    </>
  );
}
