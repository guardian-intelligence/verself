export interface BillingFlashSearch {
  purchased: boolean;
  subscribed: boolean;
}

export function parseBillingFlashSearch(search: Record<string, unknown>): BillingFlashSearch {
  return {
    purchased: search.purchased === true || search.purchased === "true",
    subscribed: search.subscribed === true || search.subscribed === "true",
  };
}
