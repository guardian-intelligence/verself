import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { emitSpan } from "~/lib/telemetry/browser";
import { SectionWorkshop } from "~/features/design/sections/workshop";
import { AppliedFooter } from "~/features/design/sections/applied-footer";
import { ogMeta } from "~/lib/head";

// /design/workshop — the canonical Workshop specimen. The page chrome is
// already Workshop (inherited from the _workshop layout), so the body just
// needs content: the section, then the Applied cross-treatment footer.

export const Route = createFileRoute("/_workshop/design/workshop")({
  component: DesignWorkshop,
  head: () => ({
    meta: ogMeta({
      slug: "design",
      title: "Workshop — Guardian brand system",
      description:
        "The Workshop treatment: Iron ground, Geist throughout, Amber as the sole accent. The productivity chrome — Fraunces absent, wings only.",
    }),
  }),
});

function DesignWorkshop() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("design.treatment.view", {
      treatment: "workshop",
      referrer_route: document.referrer ?? "",
    });
  }, []);

  return (
    <div className="mx-auto w-full max-w-[96rem] px-4 py-10 md:px-6 md:py-14">
      <SectionWorkshop />
      <AppliedFooter />
    </div>
  );
}
