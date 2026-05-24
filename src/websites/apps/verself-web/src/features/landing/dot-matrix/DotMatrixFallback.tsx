import type { CSSProperties } from "react";

import { cn } from "@verself/ui/lib/utils";

const fallbackStyle = {
  backgroundImage: "radial-gradient(circle, rgb(136 136 136 / 0.54) 1px, transparent 1.7px)",
  backgroundPosition: "center top",
  backgroundSize: "14px 14px",
} satisfies CSSProperties;

export function DotMatrixFallback({ className }: { readonly className?: string }) {
  return (
    <div
      className={cn(
        "absolute inset-0 bg-[#080909] opacity-95 [mask-image:linear-gradient(180deg,black_0%,black_72%,transparent_100%)]",
        className,
      )}
      data-dot-matrix-fallback=""
      style={fallbackStyle}
    />
  );
}
