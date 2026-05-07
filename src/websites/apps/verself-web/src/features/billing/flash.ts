import * as v from "valibot";

// FlashIntent is the client-side projection of the post-Stripe redirect
// query string. The billing-service is the authoritative producer — see
// src/services/billing-service/internal/billing/contracts.go (contractReturnURL)
// for the canonical parameter set. This module's job is to validate what
// comes in at the route boundary and collapse it into a discriminated
// union the UI renders off exhaustively.

export type FlashIntent =
  | { readonly kind: "none" }
  | { readonly kind: "credits_purchased" }
  | { readonly kind: "contract_started"; readonly targetPlanId?: string }
  | {
      readonly kind: "contract_upgrade_requested";
      readonly targetPlanId?: string;
      readonly effectiveAt?: Date;
    }
  | {
      readonly kind: "contract_upgraded";
      readonly targetPlanId?: string;
      readonly effectiveAt?: Date;
    }
  | {
      readonly kind: "contract_downgrade_scheduled";
      readonly targetPlanId?: string;
      readonly effectiveAt?: Date;
    }
  | { readonly kind: "contract_resumed"; readonly targetPlanId?: string }
  | { readonly kind: "contract_unchanged"; readonly targetPlanId?: string };

// Accepts string "true"/"false" in addition to booleans because the Stripe
// redirect round-trip stringifies every query value. The transform
// collapses them into real booleans before downstream consumers see them.
const vBoolFlag = v.union([
  v.literal(true),
  v.literal(false),
  v.pipe(
    v.literal("true"),
    v.transform(() => true),
  ),
  v.pipe(
    v.literal("false"),
    v.transform(() => false),
  ),
]);

const vContractAction = v.picklist([
  "start",
  "upgrade_requested",
  "upgrade",
  "downgrade",
  "resume",
  "unchanged",
]);

// object() not strictObject() — third-party referrer junk (utm_source,
// fbclid, ...) must pass through silently rather than blow up the route.
const vFlashSearch = v.object({
  purchased: v.optional(vBoolFlag),
  contracted: v.optional(vBoolFlag),
  contractAction: v.optional(vContractAction),
  contractEffectiveAt: v.optional(v.pipe(v.string(), v.isoTimestamp())),
  targetPlanID: v.optional(v.pipe(v.string(), v.minLength(1))),
});

export type FlashSearch = v.InferOutput<typeof vFlashSearch>;

// TanStack Router validateSearch entry point. A malformed URL falls back
// to an empty object so downstream consumers render no banner — the
// alternative (throwing) would crash the route for a broken external
// referrer, which is worse UX than a missing banner.
export function parseFlashSearch(search: Record<string, unknown>): FlashSearch {
  const result = v.safeParse(vFlashSearch, search);
  return result.success ? result.output : {};
}

// Pure projection from validated search params to the discriminated union
// the UI consumes. Separated from parseFlashSearch so components can re-run
// it on memoized search state without re-parsing.
export function projectFlashIntent(search: FlashSearch): FlashIntent {
  if (search.purchased === true) {
    return { kind: "credits_purchased" };
  }

  if (search.contracted !== true) {
    return { kind: "none" };
  }

  // exactOptionalPropertyTypes forces us to omit optional fields rather
  // than set them to undefined — conditionally spread the planId /
  // effectiveAt slices into the result.
  const planIdPart = search.targetPlanID ? { targetPlanId: search.targetPlanID } : {};
  const parsedEffectiveAt = search.contractEffectiveAt
    ? parseEffectiveAt(search.contractEffectiveAt)
    : undefined;
  const effectiveAtPart = parsedEffectiveAt ? { effectiveAt: parsedEffectiveAt } : {};

  switch (search.contractAction) {
    case "start":
      return { kind: "contract_started", ...planIdPart };
    case "upgrade_requested":
      return { kind: "contract_upgrade_requested", ...planIdPart, ...effectiveAtPart };
    case "upgrade":
      return { kind: "contract_upgraded", ...planIdPart, ...effectiveAtPart };
    case "downgrade":
      return { kind: "contract_downgrade_scheduled", ...planIdPart, ...effectiveAtPart };
    case "resume":
      return { kind: "contract_resumed", ...planIdPart };
    case "unchanged":
      return { kind: "contract_unchanged", ...planIdPart };
    case undefined:
      // contracted=true with no action is treated as a plain "contract
      // started" so the banner still renders on older redirect shapes.
      return { kind: "contract_started", ...planIdPart };
  }
}

function parseEffectiveAt(value: string): Date | undefined {
  const parsed = new Date(value);
  return Number.isFinite(parsed.getTime()) ? parsed : undefined;
}
