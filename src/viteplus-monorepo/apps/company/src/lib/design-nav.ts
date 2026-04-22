// /design is a single long page. Each entry below becomes both the rail label
// and the section anchor inside the page DOM.
//
// The page has four treatments — Company, Workshop, Newsroom, Letters — each
// self-declaring its own palette, typography, and mark usage. Photography and
// Business cards are two cross-treatment Applied rules. System-level shared
// invariants (Argent wings, SIL OFL type families) live in the page header,
// not as numbered sections — the treatments carry the language by rendering
// it, not by listing it separately.
export type DesignSectionId =
  | "company"
  | "workshop"
  | "newsroom"
  | "letters"
  | "photography"
  | "business-cards";

export type DesignSection = {
  readonly id: DesignSectionId;
  readonly number: string;
  readonly group: "Treatments" | "Applied";
  readonly label: string;
  readonly title: string;
};

export const DESIGN_SECTIONS: readonly DesignSection[] = [
  {
    id: "company",
    number: "01",
    group: "Treatments",
    label: "Company",
    title: "Company — the record.",
  },
  {
    id: "workshop",
    number: "02",
    group: "Treatments",
    label: "Workshop",
    title: "Workshop — where the work happens.",
  },
  {
    id: "newsroom",
    number: "03",
    group: "Treatments",
    label: "Newsroom",
    title: "Newsroom — the broadcast.",
  },
  {
    id: "letters",
    number: "04",
    group: "Treatments",
    label: "Letters",
    title: "Letters — the long form.",
  },
  {
    id: "photography",
    number: "05",
    group: "Applied",
    label: "Photography",
    title: "Argent needs a floor.",
  },
  {
    id: "business-cards",
    number: "06",
    group: "Applied",
    label: "Business Cards",
    title: "3.5 × 2 inches.",
  },
];

export const DESIGN_GROUPS: readonly DesignSection["group"][] = ["Treatments", "Applied"];
