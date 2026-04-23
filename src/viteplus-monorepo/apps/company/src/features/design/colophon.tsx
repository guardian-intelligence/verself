import type { ReactNode } from "react";

// Colophon — a magazine-style specifications block rendered on Vellum. Used
// where a treatment needs a quiet recess to carry the metrics (hex codes,
// type specs, pantones) without the console-style dark panel reading as
// terminal chrome.
//
// The vessel: Vellum background, 1 px Stone hairline, generous padding,
// Geist 13/1.55 for rule prose, Geist Mono 11/1.35 for metric values. The
// visual reference is The New York Review of Books' masthead footer and
// Harper's colophons — a block of facts that still feels editorial.

export interface ColophonRow {
  readonly label: string;
  readonly value: string;
  readonly note?: string;
}

export interface ColophonProps {
  readonly heading?: string;
  readonly rows: readonly ColophonRow[];
  readonly footer?: ReactNode;
}

export function Colophon({ heading = "Colophon", rows, footer }: ColophonProps) {
  return (
    <div
      data-block="colophon"
      style={{
        background: "var(--color-vellum)",
        border: "1px solid rgba(11,11,11,0.12)",
        borderRadius: "10px",
        padding: "28px 32px",
        color: "var(--color-ink)",
      }}
    >
      <div
        style={{
          fontFamily: "'Geist Mono', ui-monospace, monospace",
          fontWeight: 600,
          fontVariationSettings: '"wght" 600',
          fontSize: "10px",
          letterSpacing: "0.18em",
          textTransform: "uppercase",
          color: "rgba(11,11,11,0.55)",
          marginBottom: "16px",
        }}
      >
        {heading}
      </div>
      <dl
        style={{
          display: "grid",
          gridTemplateColumns: "minmax(160px, 1fr) minmax(0, 2fr)",
          columnGap: "28px",
          rowGap: "10px",
          margin: 0,
          fontFamily: "'Geist', sans-serif",
          fontSize: "13px",
          lineHeight: 1.55,
        }}
      >
        {rows.map((row) => (
          <div key={row.label} style={{ display: "contents" }}>
            <dt
              style={{
                fontFamily: "'Geist', sans-serif",
                color: "rgba(11,11,11,0.6)",
              }}
            >
              {row.label}
            </dt>
            <dd style={{ margin: 0 }}>
              <span
                style={{
                  fontFamily: "'Geist Mono', ui-monospace, monospace",
                  fontSize: "12px",
                  letterSpacing: "0.04em",
                  color: "var(--color-ink)",
                }}
              >
                {row.value}
              </span>
              {row.note ? (
                <span
                  style={{
                    color: "rgba(11,11,11,0.6)",
                    marginLeft: "12px",
                    fontSize: "12px",
                  }}
                >
                  {row.note}
                </span>
              ) : null}
            </dd>
          </div>
        ))}
      </dl>
      {footer ? (
        <div
          style={{
            marginTop: "18px",
            paddingTop: "14px",
            borderTop: "1px solid rgba(11,11,11,0.10)",
            fontFamily: "'Geist', sans-serif",
            fontSize: "12px",
            lineHeight: 1.5,
            color: "rgba(11,11,11,0.6)",
          }}
        >
          {footer}
        </div>
      ) : null}
    </div>
  );
}
