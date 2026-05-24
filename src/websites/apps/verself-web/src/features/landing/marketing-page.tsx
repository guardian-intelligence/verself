import { captureDotMatrixPointer, DotMatrixField, releaseDotMatrixPointer } from "./dot-matrix";

export function MarketingLandingPage() {
  return (
    <main
      className="relative isolate min-h-[100svh] overflow-hidden bg-[#080909]"
      onPointerCancel={releaseDotMatrixPointer}
      onPointerDown={captureDotMatrixPointer}
      onPointerLeave={releaseDotMatrixPointer}
      onPointerMove={captureDotMatrixPointer}
      onPointerUp={releaseDotMatrixPointer}
    >
      <div className="fixed inset-0 -z-20">
        <DotMatrixField />
      </div>
      <div
        aria-hidden="true"
        className="fixed inset-0 -z-10 bg-[radial-gradient(circle_at_50%_44%,rgba(255,255,255,0.055),transparent_34%),linear-gradient(180deg,rgba(8,9,9,0.16)_0%,rgba(8,9,9,0.28)_48%,#080909_100%)]"
      />
    </main>
  );
}
