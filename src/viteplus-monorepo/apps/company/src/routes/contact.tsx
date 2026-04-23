import { createFileRoute } from "@tanstack/react-router";
import { CONTACT_META, contact } from "~/content/contact";
import { BodyParagraph, PageShell } from "~/components/page-shell";
import { ogMeta } from "~/lib/head";

export const Route = createFileRoute("/contact")({
  component: ContactPage,
  head: () => ({
    meta: ogMeta({
      slug: "contact",
      title: CONTACT_META.title,
      description: CONTACT_META.description,
    }),
  }),
});

function ContactPage() {
  return (
    <PageShell kicker={contact.kicker} heading={contact.hero}>
      <BodyParagraph>{contact.intro}</BodyParagraph>

      <ul className="mt-6 flex flex-col gap-4">
        {contact.channels.map((channel) => (
          <li
            key={channel.email}
            className="flex flex-col gap-1 rounded-lg p-5"
            style={{
              border: "1px solid var(--treatment-surface-border)",
              background: "var(--treatment-surface-subtle)",
            }}
          >
            <span
              className="font-mono text-[10px] uppercase tracking-[0.18em]"
              style={{ color: "var(--treatment-muted-faint)" }}
            >
              {channel.name}
            </span>
            <a
              href={`mailto:${channel.email}`}
              style={{
                color: "var(--treatment-accent)",
                fontSize: "16px",
              }}
            >
              {channel.email}
            </a>
            <span
              style={{
                fontFamily: "'Geist', sans-serif",
                fontSize: "13px",
                lineHeight: 1.55,
                color: "var(--treatment-muted)",
              }}
            >
              {channel.note}
            </span>
          </li>
        ))}
      </ul>

      <p
        className="mt-6 font-mono text-[11px] uppercase tracking-[0.16em]"
        style={{ color: "var(--treatment-muted-faint)" }}
      >
        {contact.mailingAddress}
      </p>
    </PageShell>
  );
}
