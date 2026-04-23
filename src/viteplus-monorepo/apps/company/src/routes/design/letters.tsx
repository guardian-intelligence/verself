import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { emitSpan } from "~/lib/telemetry/browser";
import { SectionLetters } from "~/features/design/sections/letters";
import { AppliedFooter } from "~/features/design/sections/applied-footer";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/design/letters")({
  component: DesignLetters,
  staticData: { treatment: "letters" as const },
  head: () => ({
    meta: ogMeta({
      slug: "design",
      title: "Letters — Guardian brand system",
      description:
        "The Letters treatment: Paper ground, Fraunces body, Bordeaux as the editorial accent. Where individual voices inside Guardian show their work.",
    }),
  }),
});

function DesignLetters() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("design.treatment.view", {
      treatment: "letters",
      referrer_treatment: "",
      referrer_route: document.referrer ?? "",
    });
  }, []);

  return (
    <div
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
        minHeight: "100vh",
      }}
    >
      <div className="mx-auto w-full max-w-[96rem] px-4 py-10 md:px-6 md:py-14">
        <SectionLetters />
      </div>
      {/* AppliedFooter is cross-treatment teaching material; render it on
          Iron (Company) ground so the reader leaves the editorial surface
          cleanly when the rules discussion begins. */}
      <div
        data-treatment="company"
        style={{
          background: "var(--treatment-ground)",
          color: "var(--treatment-ink)",
        }}
      >
        <div className="mx-auto w-full max-w-[96rem] px-4 py-10 md:px-6 md:py-14">
          <AppliedFooter />
        </div>
      </div>
    </div>
  );
}
