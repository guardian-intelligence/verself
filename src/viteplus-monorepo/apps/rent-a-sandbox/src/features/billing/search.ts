export interface BillingFlashSearch {
  purchased?: boolean;
  contracted?: boolean;
  contractAction?: "start" | "upgrade" | "downgrade" | "resume" | "unchanged";
  contractEffectiveAt?: string;
  targetPlanID?: string;
}

export function parseBillingFlashSearch(search: Record<string, unknown>): BillingFlashSearch {
  const flash: BillingFlashSearch = {};

  if (search.purchased === true || search.purchased === "true") {
    flash.purchased = true;
  }

  if (search.contracted === true || search.contracted === "true") {
    flash.contracted = true;
  }

  if (typeof search.contractAction === "string" && isContractAction(search.contractAction)) {
    flash.contractAction = search.contractAction;
  }

  if (typeof search.contractEffectiveAt === "string") {
    flash.contractEffectiveAt = search.contractEffectiveAt;
  }

  if (typeof search.targetPlanID === "string") {
    flash.targetPlanID = search.targetPlanID;
  }

  return flash;
}

function isContractAction(
  value: string,
): value is NonNullable<BillingFlashSearch["contractAction"]> {
  return ["start", "upgrade", "downgrade", "resume", "unchanged"].includes(value);
}
