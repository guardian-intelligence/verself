// Solutions index. At the company-marketing layer Guardian sells one thing
// today: Verself Platform. A Solution is the commercial bundle a customer buys.
// The concrete pieces inside a Solution — services, web apps, CLIs, SDKs —
// are its products and are described on the Solution's own surfaces, not
// here. If a second Solution ever ships, append it below and the /solutions
// route will render both.

export interface SolutionProduct {
  readonly kind: "service" | "web-app" | "cli" | "sdk";
  readonly name: string;
  readonly blurb: string;
}

export interface SolutionCard {
  readonly slug: "verself";
  readonly name: string;
  readonly kicker: string;
  readonly oneLiner: string;
  readonly description: string;
  readonly products: readonly SolutionProduct[];
  readonly href: string;
}

export const SOLUTIONS_META = {
  title: "Solutions — Guardian",
  description:
    "Verself Platform is the Guardian compute stack. Services, a web console, CLIs, and SDKs, under one sign.",
} as const;

export const SOLUTIONS: readonly SolutionCard[] = [
  {
    slug: "verself",
    name: "Verself Platform",
    kicker: "Compute",
    oneLiner: "Sandboxes, CI runners, and long-running workloads on bare metal you can see.",
    description:
      "Verself runs code. CI jobs, single-purpose VM workloads, long-running environments. Each workload gets a rehearsable boot image, a short-lived filesystem, and a receipt a founder can audit line-by-line. One tenant never sees another's bytes.",
    products: [
      {
        kind: "service",
        name: "Compute services",
        blurb:
          "Sandbox rental, billing, identity, mailbox, governance — the services a founder uses to run real customer workloads.",
      },
      {
        kind: "web-app",
        name: "Console",
        blurb:
          "The web app where a founder looks when they want to know what is happening. Workloads, bills, identity, mail, one pane.",
      },
      {
        kind: "cli",
        name: "Verself CLI",
        blurb:
          "The command-line surface to the same APIs Console calls. Every action Console can take, the CLI can script.",
      },
      {
        kind: "sdk",
        name: "Language SDKs",
        blurb:
          "Typed clients generated from the same OpenAPI definitions Console and the CLI share. One source of truth per service.",
      },
    ],
    href: "https://platform.anveio.com",
  },
];
