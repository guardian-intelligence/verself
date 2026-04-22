import { type ReactNode } from "react";

// Every non-landing route uses this shell: centered column, kicker + h1
// mirroring the landing type scale, children below. The `ground` prop swaps
// the shell's background + accent tokens — "paper" routes the editorial
// surfaces (Letters) per the brand audience split in brand/voice.md:
//   Iron for customers. Flare for the world. Paper for editorial.
// Colors below consume --shell-* CSS variables so nested components (e.g.
// BodyParagraph) inherit ground-appropriate muted colors without threading
// ground as a prop.

export type ShellGround = "iron" | "paper";

export interface PageShellProps {
  readonly kicker: string;
  readonly heading: string;
  readonly children: ReactNode;
  readonly ground?: ShellGround;
}

export function PageShell({ kicker, heading, children, ground = "iron" }: PageShellProps) {
  const shellClass = ground === "paper" ? "shell-paper" : "";
  return (
    <div
      className={shellClass}
      style={{
        background: "var(--shell-bg)",
        color: "var(--shell-fg)",
      }}
    >
      <div className="mx-auto w-full max-w-4xl px-4 py-16 md:px-6 md:py-24">
        <p
          className="font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
          style={{ color: "var(--shell-muted-faint)" }}
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
            color: "var(--shell-fg)",
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
        color: "var(--shell-muted-strong)",
        margin: 0,
      }}
    >
      {children}
    </p>
  );
}
