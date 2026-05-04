import type { LockupVariant } from "../components/lockup";

// The three treatments Guardian ships in. Every chrome-bearing surface
// declares one; the treatment binds a ground, an accent, a wordmark colour,
// a muted ramp, and a lockup variant. Consumers never mix tokens across
// treatments — the whole point of the treatment system is that "Workshop
// but with Bordeaux" is not a valid surface.
export type Treatment = "workshop" | "newsroom" | "letters";

// Per-treatment lockup variant. The mark treatment is the load-bearing brand
// signal on each ground:
//   workshop → argent  (bare cropped wings on Ink — the dark canvas is itself
//                       the clearspace, so the mark needs no carrier tile)
//   newsroom → emboss  (argent wings inside a dark circular medallion, on Argent)
//   letters  → chip    (argent wings inside a dark rounded tile, on Paper)
// Emboss carries wings across loud hero bands without fighting for contrast;
// chip carries them through editorial bookplates. Workshop stays bare because
// a tile the colour of the ground is just invisible padding beside a wordmark.
export const TREATMENT_WORDMARK_VARIANT: Record<Treatment, LockupVariant> = {
  workshop: "argent",
  newsroom: "emboss",
  letters: "chip",
};

// Default section suffix per treatment — the uppercase string that renders
// after `GUARDIAN · ` in the masthead. Workshop is the house root ("/"): no
// suffix, the mark is Guardian-the-house. Newsroom and Letters are sections:
// the suffix is the room name. Routes that want a more specific suffix
// (/design → "BRAND SYSTEM") pass it explicitly via AppChrome's `section`
// prop and override the default.
export const TREATMENT_DEFAULT_SECTION: Record<Treatment, string | undefined> = {
  workshop: undefined,
  newsroom: "Newsroom",
  letters: "Letters",
};
