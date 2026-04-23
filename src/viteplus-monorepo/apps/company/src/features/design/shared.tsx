import type { CSSProperties, ReactNode } from "react";
import { Lockup } from "@forge-metal/brand";

// Hairline colour for dark panels and dark-ground borders inside the Workshop
// dashboard specimen and the treatment Surface wrapper. The treatment
// primitives carry their own internal LINE constants.
export const LINE = "#2a2a2f";

export type DesignSectionId =
  | "workshop"
  | "newsroom"
  | "letters"
  | "photography"
  | "business-cards";

export type DesignSection = {
  readonly id: DesignSectionId;
  readonly number: string;
  readonly group: "Treatments" | "Applied";
  readonly label: string;
  readonly title: string;
};

const SECTION_META: Record<DesignSectionId, DesignSection> = {
  workshop: {
    id: "workshop",
    number: "01",
    group: "Treatments",
    label: "Workshop",
    title: "Workshop — where the work happens.",
  },
  newsroom: {
    id: "newsroom",
    number: "02",
    group: "Treatments",
    label: "Newsroom",
    title: "Newsroom — the broadcast.",
  },
  letters: {
    id: "letters",
    number: "03",
    group: "Treatments",
    label: "Letters",
    title: "Letters — the long form.",
  },
  photography: {
    id: "photography",
    number: "04",
    group: "Applied",
    label: "Photography",
    title: "Argent needs a floor.",
  },
  "business-cards": {
    id: "business-cards",
    number: "05",
    group: "Applied",
    label: "Business Cards",
    title: "3.5 × 2 inches.",
  },
};

export function sectionMeta(id: DesignSectionId): DesignSection {
  return SECTION_META[id];
}

// ============================================================================
// Surface — a flat canvas in one of the three palette grounds.
//
// Paint and scope are orthogonal axes, so Surface sets only the paint: the
// `ground` prop selects a literal background colour (iron / flare / paper)
// and a matching foreground (type-iron / ink / ink). The treatment SCOPE —
// which muted ramp, which accent, which display font resolves for children
// reading var(--treatment-*) — is inherited from the ancestor data-treatment
// wrapper. That lets a specimen compose "Flare canvas with Newsroom muted
// ramp" or "Iron canvas with Workshop muted ramp" without a new scope-switch
// hiding the paint decision.
//
// Prior versions muxed paint and scope through one data-treatment attribute,
// so `<Surface ground="flare">` set data-treatment="newsroom" and then read
// background from var(--treatment-ground). When the Newsroom scope rebound
// its ground to Paper, every Flare specimen silently rendered Paper-on-Paper.
// ============================================================================
export function Surface({
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
  const paint = {
    iron: { bg: "var(--color-iron)", fg: "var(--color-type-iron)" },
    flare: { bg: "var(--color-flare)", fg: "var(--color-ink)" },
    paper: { bg: "var(--color-paper)", fg: "var(--color-ink)" },
  }[ground];
  const border =
    ground === "iron" ? "rgba(245, 245, 245, 0.12)" : "rgba(11, 11, 11, 0.14)";
  return (
    <div
      className={className}
      data-surface={ground}
      style={{
        padding: "48px 40px",
        border: `1px solid ${border}`,
        borderRadius: "12px",
        marginBottom: "16px",
        background: paint.bg,
        color: paint.fg,
        ...style,
      }}
    >
      {children}
    </div>
  );
}

export function ScrimLabel({ kind, children }: { kind: "no" | "yes"; children: ReactNode }) {
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

export function BizCard({ ground }: { ground: "iron" | "flare" }) {
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
        variant={ground === "flare" ? "emboss" : "workshop-chip"}
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
          Founder · Guardian Intelligence
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
export const heroStyle = `
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
