import type { ReactNode } from "react";
import { Lockup, WingsArgent, WingsChip, type LockupSize, type LockupVariant } from "@forge-metal/brand";

// Mark specimen block. One big carrier cell on the treatment's ground, an
// info strip below with the hexes that make the carrier up, and optional
// ladders underneath:
//   • TreatmentSizeLadder  — how the wings scale from favicon to signage.
//   • TreatmentLockupLadder — how the wordmark-plus-mark pair behaves at
//     lg/md/sm. Only relevant to treatments that actually lock up (Company,
//     Newsroom). Workshop never pairs wings with a wordmark and Letters uses
//     a thin-ruled nameplate instead — those treatments use a different
//     ladder (TreatmentMastheadLadder for Letters, size-only for Workshop).

const LINE = "#2a2a2f";
const PANEL_BG = "#17171a";
const PANEL_2_BG = "#111113";

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
        background: PANEL_BG,
        border: `1px solid ${LINE}`,
        borderRadius: "12px",
        overflow: "hidden",
        maxWidth: `${maxWidthPx}px`,
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
              marginTop: idx === 0 ? 0 : "4px",
              textTransform: row.emphasise === "name" ? "uppercase" : undefined,
              letterSpacing: row.emphasise === "name" ? "0.08em" : undefined,
            }}
          >
            <span
              style={{ color: row.emphasise === "name" ? "var(--color-type-iron)" : undefined }}
            >
              {row.label}
            </span>
            <span
              style={{
                color:
                  row.emphasise === "hex"
                    ? "var(--color-type-iron)"
                    : row.emphasise === "name"
                      ? "var(--muted-faint)"
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
        border: `1px solid ${LINE}`,
        borderRadius: "12px",
        background: PANEL_BG,
        flexWrap: "wrap",
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
              color: "var(--muted-faint)",
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
}: {
  readonly groundVar?: string;
  readonly rows: readonly LockupLadderRow[];
  readonly footer?: ReactNode;
}) {
  return (
    <div
      style={{
        padding: "28px 32px",
        border: `1px solid ${LINE}`,
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
      {footer ? (
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
        border: `1px solid ${LINE}`,
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
              color: "var(--muted-faint)",
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
            color: "var(--muted)",
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
        border: `1px solid ${LINE}`,
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
              color: "var(--shell-muted-meta, rgba(11,11,11,0.6))",
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

// Shared Letters nameplate. Wings (via the iron chip, since the nameplate
// lives on Paper) + "GUARDIAN LETTERS" tracked uppercase Geist + 1 px
// Bordeaux rule below. Explicitly NOT a Lockup: we do not want Fraunces at
// masthead scale competing with the article H1. The nameplate reads as a
// volume masthead (Paris Review / The Baffler / Harper's) so the H1 does the
// heading work.
export function Nameplate() {
  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: "14px",
          paddingBottom: "10px",
          borderBottom: "1px solid var(--color-bordeaux)",
          color: "var(--color-ink)",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: "10px" }}>
          <WingsChip style={{ width: "22px", height: "22px", flex: "0 0 22px" }} />
          <span
            style={{
              fontFamily: "'Geist', sans-serif",
              fontWeight: 600,
              fontSize: "11px",
              letterSpacing: "0.26em",
              textTransform: "uppercase",
              color: "var(--color-ink)",
            }}
          >
            Guardian · Letters
          </span>
        </div>
      </div>
    </div>
  );
}
