import type { CSSProperties, ReactNode } from "react";
import { Eyebrow } from "~/components/eyebrow";
import type { DesignSection } from "./shared";

export function Section({
  meta,
  lede,
  children,
  contentStyle,
}: {
  readonly meta: DesignSection;
  readonly lede: ReactNode;
  readonly children: ReactNode;
  readonly contentStyle?: CSSProperties;
}) {
  return (
    <section id={meta.id} className="mb-24 scroll-mt-[var(--header-scroll-offset)]">
      <Eyebrow style={{ marginBottom: "14px" }}>
        {meta.number} · {meta.group} — {meta.label}
      </Eyebrow>
      <h2
        className="font-display"
        style={{
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: "32px",
          lineHeight: 1.1,
          letterSpacing: "-0.02em",
          margin: "0 0 8px",
          color: "var(--treatment-ink)",
          textTransform: "none",
        }}
      >
        {meta.title}
      </h2>
      <p
        className="font-sans"
        style={{
          color: "var(--treatment-muted-meta)",
          maxWidth: "760px",
          margin: "0 0 24px",
          fontSize: "14px",
          lineHeight: 1.55,
        }}
      >
        {lede}
      </p>
      <div style={contentStyle}>{children}</div>
    </section>
  );
}

// RulesRow — the per-treatment "spec" pairing. Intentionally gated on a wide
// breakpoint (1520 px viewport, which lands the content column ≈ 1240 px
// wide): below that, the type ladder's minimum of 760 px doesn't leave
// enough room beside the mark carrier for Display samples to wrap
// comfortably, and the two primitives stack. Above that, the mark carrier
// sits left (narrow, 400 px) and the type ladder takes the right column
// with ~820 px of breathing room — enough for 64 px Fraunces display
// samples to break on a word boundary instead of mid-syllable.
//
// The previous uniform vertical stack (palette → mark → size ladder →
// lockup ladder → type ladder) marched in a 5-row column and made the
// per-treatment section taller than necessary. Rules (narrow mark carrier
// + type ladder) now read as one engineered exhibit side by side at
// ≥ 1520 px; ladders and hero surfaces stay full-width below. Responsive
// polish (≤ md) is a separate pass — this change does not preclude it,
// since the single-column fallback IS the narrow-viewport layout.
export function RulesRow({ children }: { readonly children: ReactNode }) {
  return (
    <div className="treatment-rules-row" style={{ marginBottom: "16px" }}>
      {children}
      <style>{RULES_ROW_CSS}</style>
    </div>
  );
}

const RULES_ROW_CSS = `
  .treatment-rules-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1520px) {
    .treatment-rules-row {
      grid-template-columns: minmax(0, 400px) minmax(0, 1fr);
      gap: 24px;
    }
  }
`;
