import type { LockupVariant } from "../components/lockup";

// The three treatments Guardian ships in. Every chrome-bearing surface
// declares one; the treatment binds a ground, an accent, a wordmark colour,
// a muted ramp, and a lockup variant. Consumers never mix tokens across
// treatments — the whole point of the treatment system is that "Workshop
// but with Bordeaux" is not a valid surface.
export type Treatment = "workshop" | "newsroom" | "letters";

// Per-treatment lockup variant. The chip choices are the load-bearing brand
// signal on each ground:
//   workshop → workshop-chip  (iron tile · 1 px argent border · argent wings)
//   newsroom → emboss         (argent wings inside an ink medallion)
//   letters  → chip           (argent wings inside an iron editorial chip)
// Workshop's workshop-chip separates the productivity chrome from the earlier
// unframed Argent mark so that, visually, "you are inside product" is obvious
// at a glance. Emboss carries wings across Paper-on-Paper surfaces without
// fighting for contrast; chip carries them through editorial bookplates.
export const TREATMENT_WORDMARK_VARIANT: Record<Treatment, LockupVariant> = {
  workshop: "workshop-chip",
  newsroom: "emboss",
  letters: "chip",
};
