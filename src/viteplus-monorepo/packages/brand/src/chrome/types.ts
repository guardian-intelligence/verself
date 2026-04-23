import type { LockupVariant } from "../components/lockup";

// The four treatments Guardian ships in. Every chrome-bearing surface declares
// one; the treatment binds a ground, an accent, a wordmark colour, a muted
// ramp, and a lockup variant. Consumers never mix tokens across treatments —
// the whole point of the treatment system is that "Company but with Bordeaux"
// is not a valid surface.
export type Treatment = "company" | "workshop" | "newsroom" | "letters";

// Per-treatment lockup variant. Workshop reuses Argent wings because the
// productivity chrome keeps Guardian's default identity anchor; its distinct
// signal is the Amber accent, not a different mark. Newsroom and Letters flip
// the mark to sit on their light grounds (emboss medallion on Flare; chip
// tile on Paper) because Argent wings cannot hold on #CCFF00 or #F6F4ED.
export const TREATMENT_WORDMARK_VARIANT: Record<Treatment, LockupVariant> = {
  company: "argent",
  workshop: "argent",
  newsroom: "emboss",
  letters: "chip",
};
