import type { ReactNode } from "react";

// DEV-ONLY calibration harness (DESIGN.md "Mocking"). `?...&frame=iphone17`
// wraps the card in a pixel-accurate iPhone 17 bezel so the card squircle can
// be tuned 1:1 against the real device screen radius and Dynamic Island. This
// is NEVER part of the shipped console surface — the widget itself is the
// production surface and is never wrapped in a device frame in normal use.
//
// iPhone 17 / 17 Pro logical geometry (points; the page renders 1pt → 1px):
//   • viewport            402 × 874
//   • screen corner radius 55
//   • Dynamic Island       126 × 37, 11 from the top edge
// (Apple does not publish the screen radius; 55pt is the long-established
// community-measured value for this corner family — exact enough to eyeball
// the card squircle against, which is the whole point of this overlay.)

export type FrameKind = "iphone17";

const SPECS: Record<FrameKind, { w: number; h: number; radius: number; band: number }> = {
  iphone17: { w: 402, h: 874, radius: 55, band: 12 },
};

const ISLAND = { w: 126, h: 37, top: 11 } as const;

export function IPhoneFrame({
  kind,
  children,
}: {
  readonly kind: FrameKind;
  readonly children: ReactNode;
}) {
  const s = SPECS[kind];
  return (
    <div
      // The titanium band. Outer radius = screen radius + band so the band is
      // a constant-width ring (concentric rounded rect, Apple's own rule).
      style={{
        boxSizing: "content-box",
        width: s.w,
        height: s.h,
        padding: s.band,
        borderRadius: s.radius + s.band,
        background: "linear-gradient(160deg, #2b2b2e, #4a4a4f 40%, #1d1d1f)",
        boxShadow: "0 30px 80px -20px rgba(0,0,0,0.55)",
        flex: "0 0 auto",
      }}
    >
      <div
        // The screen. Black so it matches the OLED card ground; the card sits
        // where a Live Activity sits — just under the Dynamic Island, inset
        // by the standard content margin.
        style={{
          position: "relative",
          width: s.w,
          height: s.h,
          borderRadius: s.radius,
          background: "#000",
          overflow: "hidden",
        }}
      >
        <div
          aria-hidden="true"
          // Dynamic Island landmark. On a black screen it would be invisible,
          // so it carries a hairline outline purely so the calibration eye
          // has the real island position/size to align the card against.
          style={{
            position: "absolute",
            top: ISLAND.top,
            left: "50%",
            transform: "translateX(-50%)",
            width: ISLAND.w,
            height: ISLAND.h,
            borderRadius: ISLAND.h / 2,
            background: "#0a0a0a",
            outline: "1px solid rgba(255,255,255,0.12)",
          }}
        />
        <div
          // Live Activity slot: full content width (21pt side inset), top just
          // below the island. The card's own responsive scale model fills it.
          style={{
            position: "absolute",
            top: ISLAND.top + ISLAND.h + 12,
            left: 21,
            right: 21,
          }}
        >
          {children}
        </div>
      </div>
    </div>
  );
}
