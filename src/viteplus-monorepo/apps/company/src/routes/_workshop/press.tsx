import { createFileRoute } from "@tanstack/react-router";
import { PRESS_META, press } from "~/content/press";
import { BodyParagraph, PageShell } from "~/components/page-shell";
import { emitSpan } from "~/lib/telemetry/browser";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/_workshop/press")({
  component: PressPage,
  head: () => ({
    meta: ogMeta({
      slug: "press",
      title: PRESS_META.title,
      description: PRESS_META.description,
    }),
  }),
});

function PressPage() {
  return (
    <PageShell kicker={press.kicker} heading={press.hero}>
      <BodyParagraph>{press.intro}</BodyParagraph>

      <div className="mt-6 flex flex-col gap-4">
        <a
          href={press.kitHref}
          download
          onClick={() => {
            emitSpan("company.press.kit_download", {
              "kit.href": press.kitHref,
            });
          }}
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "15px",
            fontWeight: 500,
            color: "var(--treatment-ink)",
            textDecoration: "underline",
            textDecorationThickness: "1px",
            textUnderlineOffset: "4px",
          }}
        >
          {press.kitLabel}
        </a>
        <ul
          className="list-disc pl-5"
          style={{
            color: "var(--treatment-muted)",
            fontSize: "14px",
            lineHeight: 1.55,
          }}
        >
          {press.kitContents.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      </div>

      <div className="mt-4 flex flex-col gap-2">
        <span
          className="font-mono text-[10px] uppercase tracking-[0.18em]"
          style={{ color: "var(--treatment-muted-faint)" }}
        >
          {press.contactLabel}
        </span>
        <a
          href={`mailto:${press.contactEmail}`}
          style={{
            color: "var(--treatment-ink)",
            fontSize: "16px",
            textDecoration: "underline",
            textDecorationThickness: "1px",
            textUnderlineOffset: "4px",
          }}
        >
          {press.contactEmail}
        </a>
        <p
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "14px",
            lineHeight: 1.55,
            color: "var(--treatment-muted)",
            margin: 0,
          }}
        >
          {press.contactNote}
        </p>
      </div>

      <div className="mt-8 flex flex-col gap-2">
        <span
          className="font-mono text-[10px] uppercase tracking-[0.18em]"
          style={{ color: "var(--treatment-muted-faint)" }}
        >
          Writing guide
        </span>
        <ul
          className="list-disc pl-5"
          style={{
            color: "var(--treatment-muted)",
            fontSize: "14px",
            lineHeight: 1.55,
          }}
        >
          {press.writingGuide.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ul>
      </div>
    </PageShell>
  );
}
