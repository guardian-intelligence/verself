import type { CSSProperties, ReactNode } from "react";

// Type ladder with dynamic vertical padding. Earlier iterations hard-coded
// `padding: 14px 12px` on every row, which made the 64 px display row feel
// cramped and the 10 px badge row feel over-aired. Here the top/bottom padding
// is computed per-row from the sample's font-size: `max(8, size * 0.32)`
// lands ≈20 px at 64 px, ≈8 px at 10 px. The cell `padding` style is written
// directly onto the <td>; rows only need to declare their sample size.

export type TypeLadderRow = {
  readonly sample: ReactNode;
  readonly role: string;
  readonly spec: string;
  readonly sampleStyle: CSSProperties;
  readonly sampleSizePx: number;
};

const LINE = "#2a2a2f";

export function TreatmentTypeLadder({
  rows,
  caption,
}: {
  readonly rows: readonly TypeLadderRow[];
  readonly caption?: ReactNode;
}) {
  return (
    <div
      style={{
        marginBottom: "16px",
        padding: "8px 4px 4px",
        border: `1px solid ${LINE}`,
        borderRadius: "12px",
        background: "#17171a",
        overflowX: "auto",
      }}
    >
      <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "13px", minWidth: "640px" }}>
        <thead>
          <tr>
            <th style={headCell("55%")}>Sample</th>
            <th style={headCell()}>Role</th>
            <th style={headCell()}>Spec</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => {
            const vPad = Math.max(8, Math.round(row.sampleSizePx * 0.32));
            const base: CSSProperties = {
              borderBottom: i === rows.length - 1 ? "none" : `1px solid ${LINE}`,
              textAlign: "left",
              verticalAlign: "middle",
              padding: `${vPad}px 14px`,
            };
            return (
              <tr key={i}>
                <td style={{ ...base, color: "var(--color-type-iron)" }}>
                  <span style={row.sampleStyle}>{row.sample}</span>
                </td>
                <td
                  style={{
                    ...base,
                    color: "var(--muted)",
                    fontFamily: "'Geist Mono', ui-monospace, monospace",
                    fontSize: "11px",
                    whiteSpace: "nowrap",
                  }}
                >
                  {row.role}
                </td>
                <td
                  style={{
                    ...base,
                    color: "var(--muted)",
                    fontFamily: "'Geist Mono', ui-monospace, monospace",
                    fontSize: "11px",
                    whiteSpace: "nowrap",
                  }}
                >
                  {row.spec}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {caption ? (
        <div
          style={{
            marginTop: "12px",
            padding: "12px 14px 8px",
            borderTop: `1px solid ${LINE}`,
            fontFamily: "'Geist', sans-serif",
            fontSize: "12px",
            color: "var(--muted)",
            lineHeight: 1.5,
          }}
        >
          {caption}
        </div>
      ) : null}
    </div>
  );
}

function headCell(width?: string): CSSProperties {
  return {
    width,
    borderBottom: `1px solid ${LINE}`,
    textAlign: "left",
    verticalAlign: "middle",
    padding: "10px 14px",
    font: '600 10px/1 "Geist Mono", ui-monospace, monospace',
    fontVariationSettings: '"wght" 600',
    letterSpacing: "0.12em",
    textTransform: "uppercase",
    color: "var(--muted-faint)",
  };
}
