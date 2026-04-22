import type { CSSProperties, ReactNode } from "react";
import { Eyebrow } from "~/components/eyebrow";
import type { DesignSection } from "~/lib/design-nav";

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
          color: "var(--color-type-iron)",
          textTransform: "none",
        }}
      >
        {meta.title}
      </h2>
      <p
        className="font-sans"
        style={{
          color: "var(--muted-meta)",
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
