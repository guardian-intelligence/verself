import { trace } from "@opentelemetry/api";
import * as v from "valibot";

// The billing-service is the authoritative producer of the post-Stripe
// redirect query string. See src/billing-service/internal/billing/contracts.go
// (contractReturnURL) for the canonical parameter set. This schema is a
// client-side projection: Valibot validates the raw search, then
// projectFlashIntent collapses the result into a discriminated union the UI
// consumes directly. Malformed URLs get tagged in the span but fall back to
// "no banner" so a broken referrer never crashes the route.

export type FlashIntent =
  | { readonly kind: "none" }
  | { readonly kind: "credits_purchased" }
  | { readonly kind: "contract_started"; readonly targetPlanId?: string }
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

// Accepts string "true"/"false" in addition to booleans because Stripe's
// return URL round-trip stringifies every query value — and the old code
// tolerated both forms. The transform collapses them into real booleans.
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

const vContractAction = v.picklist(["start", "upgrade", "downgrade", "resume", "unchanged"]);

// Use object() not strictObject() so third-party referrer junk (utm_source,
// fbclid, ...) doesn't blow up the route. Unknown keys pass through silently.
const vFlashSearch = v.object({
  purchased: v.optional(vBoolFlag),
  contracted: v.optional(vBoolFlag),
  contractAction: v.optional(vContractAction),
  contractEffectiveAt: v.optional(v.pipe(v.string(), v.isoTimestamp())),
  targetPlanID: v.optional(v.pipe(v.string(), v.minLength(1))),
});

export type FlashSearch = v.InferOutput<typeof vFlashSearch>;

// TanStack Router validateSearch entry point. Runs on both SSR and client
// navigation. Emits billing.flash.parse with flash.kind attribute so the
// banner distribution is queryable in default.otel_traces.
export function parseFlashSearch(search: Record<string, unknown>): FlashSearch {
  const tracer = trace.getTracer("rent-a-sandbox");
  return tracer.startActiveSpan("billing.flash.parse", (span) => {
    try {
      const result = v.safeParse(vFlashSearch, search);
      if (!result.success) {
        span.setAttributes({
          "billing.flash.kind": "malformed",
          "billing.flash.reason": summarizeIssues(result.issues),
        });
        return {};
      }
      const parsed = result.output;
      const intent = projectFlashIntent(parsed);
      span.setAttributes({
        "billing.flash.kind": intent.kind,
        ...(parsed.targetPlanID ? { "billing.flash.target_plan_id": parsed.targetPlanID } : {}),
      });
      return parsed;
    } finally {
      span.end();
    }
  });
}

// Pure projection from validated search params to the discriminated union
// the UI consumes. Separated from parseFlashSearch so components can re-run
// it without re-parsing when search params change.
export function projectFlashIntent(search: FlashSearch): FlashIntent {
  if (search.purchased === true) {
    return { kind: "credits_purchased" };
  }

  if (search.contracted !== true) {
    return { kind: "none" };
  }

  // exactOptionalPropertyTypes forces us to omit optional fields rather than
  // set them to undefined — spread a planId/effectiveAt slice in conditionally.
  const planIdPart = search.targetPlanID ? { targetPlanId: search.targetPlanID } : {};
  const parsedEffectiveAt = search.contractEffectiveAt
    ? parseEffectiveAt(search.contractEffectiveAt)
    : undefined;
  const effectiveAtPart = parsedEffectiveAt ? { effectiveAt: parsedEffectiveAt } : {};

  switch (search.contractAction) {
    case "start":
      return { kind: "contract_started", ...planIdPart };
    case "upgrade":
      return { kind: "contract_upgraded", ...planIdPart, ...effectiveAtPart };
    case "downgrade":
      return { kind: "contract_downgrade_scheduled", ...planIdPart, ...effectiveAtPart };
    case "resume":
      return { kind: "contract_resumed", ...planIdPart };
    case "unchanged":
      return { kind: "contract_unchanged", ...planIdPart };
    case undefined:
      // contracted=true with no action field → older billing-service response.
      // Treat as a plain "contract started" so the banner still renders.
      return { kind: "contract_started", ...planIdPart };
  }
}

function parseEffectiveAt(value: string): Date | undefined {
  const parsed = new Date(value);
  return Number.isFinite(parsed.getTime()) ? parsed : undefined;
}

function summarizeIssues(issues: readonly v.BaseIssue<unknown>[]): string {
  return issues
    .slice(0, 3)
    .map((issue) => {
      const path = issue.path?.map((p) => String(p.key ?? "")).join(".") ?? "";
      return path ? `${path}: ${issue.message}` : issue.message;
    })
    .join("; ");
}
