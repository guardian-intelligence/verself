import { createFileRoute } from "@tanstack/react-router";
import { PRESS_META, press } from "~/content/press";
import { BodyParagraph, PageShell } from "~/components/page-shell";
import { emitSpan } from "~/lib/telemetry/browser";

export const Route = createFileRoute("/press")({
  component: PressPage,
  head: () => ({
    meta: [
      { title: PRESS_META.title },
      { name: "description", content: PRESS_META.description },
    ],
  }),
});

function PressPage() {
  return (
    <PageShell kicker={press.kicker} heading={press.hero}>
      <BodyParagraph>{press.intro}</BodyParagraph>

      <div
        className="mt-6 flex flex-col gap-4 rounded-lg p-5"
        style={{
          border: "1px solid rgba(245,245,245,0.12)",
          background: "rgba(245,245,245,0.02)",
        }}
      >
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
            color: "var(--color-flare)",
          }}
        >
          {press.kitLabel}
        </a>
        <ul
          className="list-disc pl-5"
          style={{
            color: "rgba(245,245,245,0.72)",
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
          style={{ color: "rgba(245,245,245,0.45)" }}
        >
          {press.contactLabel}
        </span>
        <a
          href={`mailto:${press.contactEmail}`}
          style={{ color: "var(--color-flare)", fontSize: "16px" }}
        >
          {press.contactEmail}
        </a>
        <p
          style={{
            fontFamily: "'Geist', sans-serif",
            fontSize: "14px",
            lineHeight: 1.55,
            color: "rgba(245,245,245,0.68)",
            margin: 0,
          }}
        >
          {press.contactNote}
        </p>
      </div>

      <div className="mt-8 flex flex-col gap-2">
        <span
          className="font-mono text-[10px] uppercase tracking-[0.18em]"
          style={{ color: "rgba(245,245,245,0.45)" }}
        >
          Writing guide
        </span>
        <ul
          className="list-disc pl-5"
          style={{
            color: "rgba(245,245,245,0.7)",
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
