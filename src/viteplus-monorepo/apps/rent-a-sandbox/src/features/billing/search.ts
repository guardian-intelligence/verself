export interface BillingFlashSearch {
  purchased?: boolean;
  contracted?: boolean;
}

export function parseBillingFlashSearch(search: Record<string, unknown>): BillingFlashSearch {
  const flash: BillingFlashSearch = {};

  if (search.purchased === true || search.purchased === "true") {
    flash.purchased = true;
  }

  if (search.contracted === true || search.contracted === "true") {
    flash.contracted = true;
  }

  return flash;
}
