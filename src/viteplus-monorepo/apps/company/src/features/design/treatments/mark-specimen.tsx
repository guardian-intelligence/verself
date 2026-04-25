import type { ReactNode } from "react";
import {
  Lockup,
  WingsArgent,
  WingsChip,
  type LockupSize,
  type LockupVariant,
} from "@verself/brand";

// Mark specimen block. One big carrier cell on the treatment's ground, an
// info strip below with the hexes that make the carrier up, and optional
// ladders underneath:
//   • TreatmentSizeLadder  — how the wings scale from favicon to signage.
//   • TreatmentLockupLadder — how the wordmark-plus-mark pair behaves at
//     lg/md/sm. Only relevant to treatments that actually lock up (Company,
//     Newsroom). Workshop never pairs wings with a wordmark and Letters uses
//     a thin-ruled nameplate instead — those treatments use a different
//     ladder (TreatmentMastheadLadder for Letters, size-only for Workshop).

export type MarkCarrierRow = {
  readonly label: string;
  readonly value: string;
  readonly emphasise?: "name" | "hex";
};

export function TreatmentMarkCard({
  groundVar,
  rows,
  children,
  maxWidthPx = 420,
}: {
  readonly groundVar: string;
  readonly rows: readonly MarkCarrierRow[];
  readonly children: ReactNode;
  readonly maxWidthPx?: number;
}) {
  return (
    <div
      style={{
        background: "var(--treatment-surface-subtle)",
        border: "1px solid var(--treatment-surface-border)",
        borderRadius: "12px",
        overflow: "hidden",
        maxWidth: `${maxWidthPx}px`,
        color: "var(--treatment-ink)",
      }}
    >
      <div
        style={{
          background: groundVar,
          aspectRatio: "1 / 1",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          padding: "24px",
        }}
      >
        {children}
      </div>
      <div
        style={{
          font: '600 10px/1.4 "Geist Mono", ui-monospace, monospace',
          fontVariationSettings: '"wght" 600',
          padding: "12px 14px",
          color: "var(--treatment-muted)",
          background: "var(--treatment-surface-subtle)",
          borderTop: "1px solid var(--treatment-surface-border)",
        }}
      >
        {rows.map((row, idx) => (
          <div
            key={idx}
            style={{
              display: "flex",
              justifyContent: "space-between",
              marginTop: idx === 0 ? 0 : "4px",
              textTransform: row.emphasise === "name" ? "uppercase" : undefined,
              letterSpacing: row.emphasise === "name" ? "0.08em" : undefined,
            }}
          >
            <span style={{ color: row.emphasise === "name" ? "var(--treatment-ink)" : undefined }}>
              {row.label}
            </span>
            <span
              style={{
                color:
                  row.emphasise === "hex"
                    ? "var(--treatment-ink)"
                    : row.emphasise === "name"
                      ? "var(--treatment-muted-faint)"
                      : undefined,
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

// Size ladder — wings at discrete px sizes, using the WingsChip so the glyph
// always carries its ground regardless of the surrounding panel. Consumed by
// treatments whose mark ships at a wide size range (Company, Workshop).
export function TreatmentSizeLadder({
  sizes = [16, 24, 32, 48, 64, 96, 128],
}: {
  readonly sizes?: readonly number[];
}) {
  return (
    <div
      style={{
        display: "flex",
        gap: "16px",
        alignItems: "flex-end",
        padding: "28px 24px",
        border: "1px solid var(--treatment-surface-border)",
        borderRadius: "12px",
        background: "var(--treatment-surface-subtle)",
        flexWrap: "wrap",
        color: "var(--treatment-ink)",
      }}
    >
      {sizes.map((px) => (
        <div
          key={px}
          style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: "8px" }}
        >
          <WingsChip
            style={{ display: "block", borderRadius: "4px", width: `${px}px`, height: `${px}px` }}
          />
          <small
            style={{
              font: '600 10px/1 "Geist Mono", ui-monospace, monospace',
              fontVariationSettings: '"wght" 600',
              color: "var(--treatment-muted-faint)",
              letterSpacing: "0.1em",
            }}
          >
            {px}
          </small>
        </div>
      ))}
    </div>
  );
}

export type LockupLadderRow = {
  readonly size: LockupSize;
  readonly variant?: LockupVariant;
  readonly wordmark?: ReactNode;
  readonly wordmarkColor?: string;
  readonly markPx: number;
  readonly gap: string;
  readonly role: "ceiling" | "proportional" | "floor";
};

export function TreatmentLockupLadder({
  groundVar = "var(--color-iron)",
  rows,
  footer,
  accentColor = "var(--color-flare)",
  metaColor = "var(--treatment-muted)",
  footerColor = "var(--treatment-muted-faint)",
  borderColor = "#2a2a2f",
}: {
  readonly groundVar?: string;
  readonly rows: readonly LockupLadderRow[];
  readonly footer?: ReactNode;
  // The metadata column at the right of each row ("mark 96 px · gap 18 px ·
  // ceiling") has to read against whatever ground the ladder is rendered on.
  // On Iron we use Ash-muted + Flare for the gap; on Flare we switch to
  // Ink-muted + Iron for the gap (because Flare-on-Flare is invisible).
  readonly accentColor?: string;
  readonly metaColor?: string;
  readonly footerColor?: string;
  readonly borderColor?: string;
}) {
  return (
    <div
      style={{
        padding: "28px 32px",
        border: `1px solid ${borderColor}`,
        borderRadius: "12px",
        background: groundVar,
        display: "grid",
        gap: "24px",
        overflowX: "auto",
      }}
    >
      {rows.map((row) => (
        <div
          key={row.markPx}
          style={{
            display: "grid",
            gridTemplateColumns: "1fr 220px",
            alignItems: "center",
            gap: "12px 24px",
          }}
        >
          <Lockup
            size={row.size}
            {...(row.variant ? { variant: row.variant } : {})}
            {...(row.wordmark !== undefined ? { wordmark: row.wordmark } : {})}
            {...(row.wordmarkColor ? { wordmarkColor: row.wordmarkColor } : {})}
          />
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: "4px",
              font: '600 11px/1.2 "Geist Mono", ui-monospace, monospace',
              fontVariationSettings: '"wght" 600',
              letterSpacing: "0.12em",
              textTransform: "uppercase",
              color: metaColor,
              textAlign: "right",
            }}
          >
            <span>mark {row.markPx} px</span>
            <span>
              gap{" "}
              <b style={{ color: accentColor, fontWeight: 600, letterSpacing: "0.08em" }}>
                {row.gap}
              </b>{" "}
              · {row.role}
            </span>
          </div>
        </div>
      ))}
      {footer ? (
        <div
          style={{
            borderTop: `1px solid ${borderColor}`,
            paddingTop: "16px",
            font: '600 10px/1.4 "Geist Mono", ui-monospace, monospace',
            fontVariationSettings: '"wght" 600',
            letterSpacing: "0.14em",
            textTransform: "uppercase",
            color: footerColor,
          }}
        >
          {footer}
        </div>
      ) : null}
    </div>
  );
}

// Wings-only ladder for Workshop, which never pairs wings with a wordmark.
// A vertical strip of progressively smaller Argent wings over the Iron ground,
// plus a note about the 22 px chrome anchor that ships in the live console.
export function TreatmentWingsOnlyLadder({
  sizes = [64, 48, 32, 22, 16],
  note,
}: {
  readonly sizes?: readonly number[];
  readonly note?: ReactNode;
}) {
  return (
    <div
      style={{
        padding: "28px 32px",
        border: "1px solid #2a2a2f",
        borderRadius: "12px",
        background: "var(--color-iron)",
        display: "flex",
        alignItems: "flex-end",
        gap: "28px",
        flexWrap: "wrap",
      }}
    >
      {sizes.map((px) => (
        <div
          key={px}
          style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: "8px" }}
        >
          <WingsArgent style={{ width: `${px}px`, height: "auto" }} cropped />
          <small
            style={{
              font: '600 10px/1 "Geist Mono", ui-monospace, monospace',
              fontVariationSettings: '"wght" 600',
              color: "var(--treatment-muted-faint)",
              letterSpacing: "0.1em",
            }}
          >
            {px}
          </small>
        </div>
      ))}
      {note ? (
        <div
          style={{
            marginLeft: "auto",
            maxWidth: "260px",
            fontFamily: "'Geist', sans-serif",
            fontSize: "12px",
            color: "var(--treatment-muted)",
            lineHeight: 1.5,
          }}
        >
          {note}
        </div>
      ) : null}
    </div>
  );
}

// Masthead ladder for Letters — a progression of thin-ruled nameplates rather
// than a scaling wordmark lockup. The masthead uses small wings + tracked
// uppercase "GUARDIAN LETTERS" with a Bordeaux rule underneath, which is the
// pattern Letters' top-of-page uses; the ladder demonstrates how the same
// device behaves at three card widths (lg/md/sm).
export type MastheadLadderRow = {
  readonly widthPx: number;
  readonly issue: string;
  readonly date: string;
  readonly label: "ceiling" | "proportional" | "floor";
};

export function TreatmentMastheadLadder({
  rows,
  footer,
}: {
  readonly rows: readonly MastheadLadderRow[];
  readonly footer?: ReactNode;
}) {
  return (
    <div
      style={{
        padding: "28px 32px",
        border: "1px solid rgba(11,11,11,0.14)",
        borderRadius: "12px",
        background: "var(--color-paper)",
        display: "grid",
        gap: "28px",
      }}
    >
      {rows.map((row) => (
        <div
          key={row.widthPx}
          style={{
            display: "flex",
            flexDirection: "column",
            gap: "6px",
            maxWidth: `${row.widthPx}px`,
          }}
        >
          <Nameplate />
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              gap: "16px",
              paddingTop: "6px",
              fontFamily: "'Geist Mono', ui-monospace, monospace",
              fontSize: "10px",
              color: "var(--treatment-muted-meta, rgba(11,11,11,0.6))",
              letterSpacing: "0.14em",
              textTransform: "uppercase",
            }}
          >
            <span>{row.issue}</span>
            <span>
              width {row.widthPx} px · {row.label}
            </span>
          </div>
        </div>
      ))}
      {footer ? (
        <div
          style={{
            borderTop: `1px solid rgba(11,11,11,0.12)`,
            paddingTop: "14px",
            font: '600 10px/1.4 "Geist Mono", ui-monospace, monospace',
            fontVariationSettings: '"wght" 600',
            letterSpacing: "0.14em",
            textTransform: "uppercase",
            color: "rgba(11,11,11,0.55)",
          }}
        >
          {footer}
        </div>
      ) : null}
    </div>
  );
}

// Shared Letters nameplate. The Lockup at size sm — Guardian's canonical
// masthead — plus a 1 px Ink rule below. Before the Geist cutover this
// rendered a hand-rolled wings + tracked-uppercase pair because the
// top-of-page Lockup was Fraunces and competed with the article H1; now
// that the Lockup IS the quiet volume marker, the nameplate delegates to
// it so the chrome masthead and the /design/letters specimen stay
// identical by construction. The 1 px rule is what makes this a nameplate
// and not just a lockup. (Rule was previously Bordeaux; retreated to Ink
// 2026-04-23 so Bordeaux stays reserved for the blockquote left-rule.)
export function Nameplate() {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: "14px",
        paddingBottom: "10px",
        borderBottom: "1px solid var(--color-ink)",
        color: "var(--color-ink)",
      }}
    >
      <Lockup size="sm" variant="chip" section="Letters" wordmarkColor="var(--color-ink)" />
    </div>
  );
}
