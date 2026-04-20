// Products index. Translates the Guardian house into three product names:
//   Metal    — the sandbox/CI/compute console (apps/platform today,
//              moving to console.<domain> later).
//   Console  — the operator-agnostic control plane that lives inside Metal
//              for day-to-day work.
//   Letters  — the long-form editorial surface (deployed as apps/letters).
//
// Product copy here never surfaces a technology name (Zitadel, Firecracker,
// ZFS, Stalwart, OpenBao) — voice rule memory feedback_no_tech_names_in_ui.

export interface ProductCard {
  readonly slug: "metal" | "console" | "letters";
  readonly name: string;
  readonly kicker: string;
  readonly oneLiner: string;
  readonly description: string;
  readonly href: string;
}

export const PRODUCTS_META = {
  title: "Products — Guardian Intelligence",
  description:
    "Metal runs the work. Console runs the founder. Letters runs the record. One house, three products.",
} as const;

export const PRODUCTS: readonly ProductCard[] = [
  {
    slug: "metal",
    name: "Metal",
    kicker: "Compute",
    oneLiner: "Sandboxes, CI runners, and long-running workloads on bare metal you can see.",
    description:
      "Metal runs code. CI jobs, single-purpose VM workloads, long-running environments. Each workload gets a rehearsable boot image, a short-lived filesystem, and a receipt you can audit line-by-line. One tenant never sees another's bytes.",
    href: "https://platform.anveio.com",
  },
  {
    slug: "console",
    name: "Console",
    kicker: "Control plane",
    oneLiner: "The single pane that runs the business — workloads, billing, identity, mail.",
    description:
      "Console is where founders look when they want to know what is happening. Deployments. Customers. Bills. Mail. A day's worth of decisions in one place, every one of them backed by the same APIs customers call.",
    href: "https://platform.anveio.com",
  },
  {
    slug: "letters",
    name: "Letters",
    kicker: "Editorial",
    oneLiner: "The Guardian Intelligence editorial platform. Prose, published.",
    description:
      "Letters is where writing happens. Drafts, revisions, publishing, archives. Guardian uses it; customers can too. The tooling is the same tooling we use to publish the Dispatch.",
    href: "https://letters.anveio.com",
  },
];
