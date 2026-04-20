import { type ReactNode } from "react";

// Every non-landing route uses this shell: centered column, kicker + h1
// mirroring the landing type scale, children below. Keeping the shell here
// means route files are small and focused on content wiring.

export interface PageShellProps {
  readonly kicker: string;
  readonly heading: string;
  readonly children: ReactNode;
}

export function PageShell({ kicker, heading, children }: PageShellProps) {
  return (
    <div className="mx-auto w-full max-w-4xl px-4 py-16 md:px-6 md:py-24">
      <p
        className="font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
        style={{ color: "rgba(245,245,245,0.55)" }}
      >
        {kicker}
      </p>
      <h1
        className="mt-5 font-display"
        style={{
          fontVariationSettings: '"opsz" 96, "SOFT" 30',
          fontWeight: 400,
          fontSize: "clamp(32px, 5vw, 52px)",
          lineHeight: 1.05,
          letterSpacing: "-0.022em",
          color: "var(--color-type-iron)",
          maxWidth: "22ch",
          margin: 0,
        }}
      >
        {heading}
      </h1>
      <div className="mt-10 flex flex-col gap-5" style={{ maxWidth: "62ch" }}>
        {children}
      </div>
    </div>
  );
}

export function BodyParagraph({ children }: { children: ReactNode }) {
  return (
    <p
      style={{
        fontFamily: "'Geist', sans-serif",
        fontWeight: 400,
        fontSize: "clamp(16px, 1.6vw, 18px)",
        lineHeight: 1.55,
        color: "rgba(245,245,245,0.82)",
        margin: 0,
      }}
    >
      {children}
    </p>
  );
}
