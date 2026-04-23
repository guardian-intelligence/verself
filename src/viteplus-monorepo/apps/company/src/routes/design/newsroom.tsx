import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { emitSpan } from "~/lib/telemetry/browser";
import { SectionNewsroom } from "~/features/design/sections/newsroom";
import { AppliedFooter } from "~/features/design/sections/applied-footer";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/design/newsroom")({
  component: DesignNewsroom,
  staticData: { treatment: "newsroom" as const },
  head: () => ({
    meta: ogMeta({
      slug: "design",
      title: "Newsroom — Guardian brand system",
      description:
        "The Newsroom treatment: Flare ground, wings inside a circular emboss, Fraunces at display weight. How Guardian appears in someone else's feed.",
    }),
  }),
});

function DesignNewsroom() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("design.treatment.view", {
      treatment: "newsroom",
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
        <SectionNewsroom />
      </div>
      {/* AppliedFooter is rendered on Iron ground — it's teaching material
          about cross-treatment rules, not a Newsroom artifact. Wrapping it
          in a data-treatment="company" scope so the reader leaves Newsroom
          when the rules discussion begins. */}
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
