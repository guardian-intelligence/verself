export interface BillingFlashSearch {
  purchased?: boolean;
  subscribed?: boolean;
}

export function parseBillingFlashSearch(search: Record<string, unknown>): BillingFlashSearch {
  const flash: BillingFlashSearch = {};

  if (search.purchased === true || search.purchased === "true") {
    flash.purchased = true;
  }

  if (search.subscribed === true || search.subscribed === "true") {
    flash.subscribed = true;
  }

  return flash;
}
