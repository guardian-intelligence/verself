import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { emitSpan } from "~/lib/telemetry/browser";
import { SectionCompany } from "~/features/design/sections/company";
import { AppliedFooter } from "~/features/design/sections/applied-footer";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/design/company")({
  component: DesignCompany,
  staticData: { treatment: "company" as const },
  head: () => ({
    meta: ogMeta({
      slug: "design",
      title: "Company — Guardian brand system",
      description:
        "The Company treatment: Iron ground, Fraunces masthead, Flare single action. Where Guardian speaks on the record.",
    }),
  }),
});

function DesignCompany() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("design.treatment.view", {
      treatment: "company",
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
        <SectionCompany />
        <AppliedFooter />
      </div>
    </div>
  );
}
