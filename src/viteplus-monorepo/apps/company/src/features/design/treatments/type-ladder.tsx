import type { CSSProperties, ReactNode } from "react";

// Type ladder — the per-treatment exhibit that shows how each typographic
// register sets.
//
// Two engineering choices worth stating up top:
//
// 1. Fixed column widths via `table-layout: fixed`. Auto table sizing let the
//    SPEC column's right edge drift by row because each row's spec string has
//    a different length; locking columns to a shared grid (55% Sample, 96 px
//    Role, 1fr Spec) aligns the right edge across rows — a reader comparing
//    ladders across Company/Workshop/Newsroom/Letters sees one spec column,
//    not four wobbling ones.
//
// 2. Padding curve tuned for breathing at mid-range. `max(10, round(8 + size *
//    0.22))` replaces the earlier `max(8, size * 0.32)`: lower slope + higher
//    floor means a 10 px badge row lands ~10 px padding (was 8) and a 32 px
//    section header lands ~15 px (was ~10) — the 20–32 px band was where the
//    old curve felt cramped, and the display row at 64 px still sits at
//    ~22 px padding, plenty of air for the Fraunces opsz 144 ascenders.
//
// 3. Spec strings split on the first middot. Everything to the left of the
//    first " · " lists the metric group (family / size / lh / tracking);
//    everything to the right lists the style group (opsz · SOFT · weight ·
//    case). The two sub-columns get their own cell so the numeric columns in
//    the metric group tabularly align across rows (`font-variant-numeric:
//    tabular-nums` enforces this) while the style group can vary freely.

export type TypeLadderRow = {
  readonly sample: ReactNode;
  readonly role: string;
  readonly spec: string;
  readonly sampleStyle: CSSProperties;
  readonly sampleSizePx: number;
};

const LINE = "#2a2a2f";

const SPEC_CELL_STYLE: CSSProperties = {
  color: "var(--treatment-muted)",
  fontFamily: "'Geist Mono', ui-monospace, monospace",
  fontSize: "11px",
  whiteSpace: "nowrap",
  fontVariantNumeric: "tabular-nums",
  fontFeatureSettings: '"tnum" 1',
};

function splitSpec(spec: string): { readonly metric: string; readonly style: string } {
  const sep = " · ";
  const idx = spec.indexOf(sep);
  if (idx === -1) return { metric: spec, style: "" };
  return {
    metric: spec.slice(0, idx),
    style: spec.slice(idx + sep.length),
  };
}

export function TreatmentTypeLadder({
  rows,
  caption,
}: {
  readonly rows: readonly TypeLadderRow[];
  readonly caption?: ReactNode;
}) {
  return (
    <div
      data-treatment="company"
      style={{
        marginBottom: "16px",
        padding: "8px 4px 4px",
        border: `1px solid ${LINE}`,
        borderRadius: "12px",
        background: "#17171a",
        overflowX: "auto",
        color: "var(--treatment-ink)",
      }}
    >
      <table
        style={{
          width: "100%",
          borderCollapse: "collapse",
          // Auto layout lets Sample keep the dominant share of width while
          // Role / Metric / Style collapse to their nowrap content widths.
          // table-layout: fixed fought 64 px Fraunces samples for horizontal
          // room; auto layout plus nowrap on the mono columns gives Sample
          // the remainder implicitly, and the three mono columns still line
          // up rigidly because every row carries the same `whiteSpace:
          // nowrap` + `tnum` rendering. 760 px min-width keeps it legible
          // inside Workshop's narrower wrap; the parent's overflow-x: auto
          // kicks in below that.
          tableLayout: "auto",
          fontSize: "13px",
          minWidth: "760px",
        }}
      >
        <thead>
          <tr>
            <th style={headCell()}>Sample</th>
            <th style={headCell()}>Role</th>
            <th style={headCell()}>Metric</th>
            <th style={headCell()}>Style</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => {
            // Log-leaning pad: breathes at 20–32 px without letting 64 px
            // blow out the row. 10 px floor keeps the smallest badge rows
            // from looking cramped against the mono spec column.
            const vPad = Math.max(10, Math.round(8 + row.sampleSizePx * 0.22));
            const base: CSSProperties = {
              borderBottom: i === rows.length - 1 ? "none" : `1px solid ${LINE}`,
              textAlign: "left",
              verticalAlign: "middle",
              padding: `${vPad}px 14px`,
            };
            const { metric, style } = splitSpec(row.spec);
            return (
              <tr key={i}>
                <td
                  style={{
                    ...base,
                    color: "var(--color-type-iron)",
                    // `break-word` lets long Display samples wrap at word
                    // boundaries first and only breaks mid-word as a last
                    // resort. `anywhere` (previous value) was too eager —
                    // "application" would split into "applicatio / n" when
                    // the Sample column got tight. `break-word` prefers
                    // "The application / layer is the product.", which
                    // reads like the original layout.
                    overflowWrap: "break-word",
                    wordBreak: "normal",
                    hyphens: "none",
                    whiteSpace: "normal",
                  }}
                >
                  <span style={row.sampleStyle}>{row.sample}</span>
                </td>
                <td
                  style={{
                    ...base,
                    color: "var(--treatment-muted)",
                    fontFamily: "'Geist Mono', ui-monospace, monospace",
                    fontSize: "11px",
                    whiteSpace: "nowrap",
                  }}
                >
                  {row.role}
                </td>
                <td style={{ ...base, ...SPEC_CELL_STYLE }}>{metric}</td>
                <td
                  style={{
                    ...base,
                    ...SPEC_CELL_STYLE,
                    color: "var(--treatment-muted-faint)",
                  }}
                >
                  {style}
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
            color: "var(--treatment-muted)",
            lineHeight: 1.5,
          }}
        >
          {caption}
        </div>
      ) : null}
    </div>
  );
}

function headCell(): CSSProperties {
  return {
    borderBottom: `1px solid ${LINE}`,
    textAlign: "left",
    verticalAlign: "middle",
    padding: "10px 14px",
    font: '600 10px/1 "Geist Mono", ui-monospace, monospace',
    fontVariationSettings: '"wght" 600',
    letterSpacing: "0.12em",
    textTransform: "uppercase",
    color: "var(--treatment-muted-faint)",
  };
}
