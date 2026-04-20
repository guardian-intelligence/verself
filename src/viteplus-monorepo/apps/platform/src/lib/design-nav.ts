// /design is a single long page (mirrors logo-playground.html). Each entry below
// becomes both the rail label and the section anchor inside the page DOM.
// Section numbering matches the playground so cross-references stay stable.
export type DesignSectionId =
  | "mark"
  | "audience"
  | "clear-space"
  | "size-ladder"
  | "lockups"
  | "product-marque"
  | "colour"
  | "typography"
  | "hero-iron"
  | "hero-flare"
  | "dispatch"
  | "product"
  | "photography"
  | "og-card"
  | "business-cards"
  | "email-signature";

export type DesignSection = {
  readonly id: DesignSectionId;
  readonly number: string;
  readonly group: "Identity" | "System" | "Applied";
  readonly label: string;
  readonly title: string;
};

export const DESIGN_SECTIONS: readonly DesignSection[] = [
  { id: "mark", number: "01", group: "Identity", label: "The mark", title: "Two wings." },
  {
    id: "audience",
    number: "02",
    group: "Identity",
    label: "Audience split",
    title: "Two marks. Two jobs.",
  },
  {
    id: "clear-space",
    number: "03",
    group: "Identity",
    label: "Clear space",
    title: "Clear space is one wing.",
  },
  {
    id: "size-ladder",
    number: "04",
    group: "Identity",
    label: "Size ladder",
    title: "From favicon to signage.",
  },
  {
    id: "lockups",
    number: "05",
    group: "Identity",
    label: "Lockups",
    title: "The wordmark, four ways.",
  },
  {
    id: "product-marque",
    number: "06",
    group: "Identity",
    label: "Product marque",
    title: "One house. Many products.",
  },
  {
    id: "colour",
    number: "07",
    group: "System",
    label: "Colour",
    title: "Three grounds. Two accents.",
  },
  {
    id: "typography",
    number: "08",
    group: "System",
    label: "Typography",
    title: "Three families. Three roles. Zero licences.",
  },
  {
    id: "hero-iron",
    number: "09",
    group: "Applied",
    label: "Hero · Iron",
    title: "Customer-facing surface.",
  },
  {
    id: "hero-flare",
    number: "10",
    group: "Applied",
    label: "Hero · Flare",
    title: "World-facing surface.",
  },
  {
    id: "dispatch",
    number: "11",
    group: "Applied",
    label: "Dispatch",
    title: "The Dispatch, on Paper.",
  },
  {
    id: "product",
    number: "12",
    group: "Applied",
    label: "Product chrome",
    title: "The serif stays. The rest is work.",
  },
  {
    id: "photography",
    number: "13",
    group: "Applied",
    label: "Photography · scrim",
    title: "Argent needs a floor.",
  },
  {
    id: "og-card",
    number: "14",
    group: "Applied",
    label: "OG card",
    title: "1200 × 630.",
  },
  {
    id: "business-cards",
    number: "15",
    group: "Applied",
    label: "Business cards",
    title: "3.5 × 2 inches.",
  },
  {
    id: "email-signature",
    number: "16",
    group: "Applied",
    label: "Email signature",
    title: "Inline, in other people's inboxes.",
  },
];

export const DESIGN_GROUPS: readonly DesignSection["group"][] = ["Identity", "System", "Applied"];
