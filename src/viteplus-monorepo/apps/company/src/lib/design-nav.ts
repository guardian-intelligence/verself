// /design is a single long page. Each entry below becomes both the rail label
// and the section anchor inside the page DOM.
//
// The page has one idea per section: The Mark carries the brand (including
// carriers, size ladder, and lockup behavior); each Treatment is a mode the
// brand operates in and documents its own palette inline; Applied shows the
// few surfaces where the marks escape the product chrome.
export type DesignSectionId =
  | "mark"
  | "typography"
  | "company"
  | "newsroom"
  | "letters"
  | "workshop"
  | "photography"
  | "business-cards";

export type DesignSection = {
  readonly id: DesignSectionId;
  readonly number: string;
  readonly group: "System" | "Treatments" | "Applied";
  readonly label: string;
  readonly title: string;
};

export const DESIGN_SECTIONS: readonly DesignSection[] = [
  { id: "mark", number: "01", group: "System", label: "The mark", title: "Two wings, four treatments." },
  {
    id: "typography",
    number: "02",
    group: "System",
    label: "Typography",
    title: "Three families. Three roles. Zero licences.",
  },
  {
    id: "company",
    number: "03",
    group: "Treatments",
    label: "Company",
    title: "Company — the record.",
  },
  {
    id: "workshop",
    number: "04",
    group: "Treatments",
    label: "Workshop",
    title: "Workshop — where the work happens.",
  },
  {
    id: "newsroom",
    number: "05",
    group: "Treatments",
    label: "Newsroom",
    title: "Newsroom — the broadcast.",
  },
  {
    id: "letters",
    number: "06",
    group: "Treatments",
    label: "Letters",
    title: "Letters — the long form.",
  },
  {
    id: "photography",
    number: "07",
    group: "Applied",
    label: "Photography · scrim",
    title: "Argent needs a floor.",
  },
  {
    id: "business-cards",
    number: "08",
    group: "Applied",
    label: "Business cards",
    title: "3.5 × 2 inches.",
  },
];

export const DESIGN_GROUPS: readonly DesignSection["group"][] = [
  "System",
  "Treatments",
  "Applied",
];
