import {
  Body,
  Container,
  Head,
  Heading,
  Hr,
  Html,
  Preview,
  Section,
  Text,
} from "@react-email/components";
import * as React from "react";

export interface AgentNotificationProps {
  subject: string;
  body: string;
}

const h = React.createElement;

// A deliberately restrained, single-column notice from a Verself agent to the
// operator. Authored with React.createElement rather than JSX so the render
// binary runs directly under Node's type-stripping (no JSX transform step).
// Rendered once at build time (see render-template.ts) with Go text/template
// actions as prop values; the braces survive HTML escaping so the output
// doubles as the template email-service ultimately sends.
export function AgentNotification({ subject, body }: AgentNotificationProps) {
  return h(
    Html,
    { lang: "en" },
    h(Head),
    h(Preview, null, subject),
    h(
      Body,
      { style: main },
      h(
        Container,
        { style: container },
        h(Text, { style: eyebrow }, "VERSELF AGENT"),
        h(Heading, { style: heading }, subject),
        h(Hr, { style: rule }),
        h(Section, null, h(Text, { style: message }, body)),
        h(Hr, { style: rule }),
        h(Text, { style: footer }, "Sent by an agent working in the verself.sh repository."),
      ),
    ),
  );
}

export default AgentNotification;

const main: React.CSSProperties = {
  backgroundColor: "#0b0b0c",
  color: "#e7e7ea",
  fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace",
  margin: 0,
  padding: "32px 0",
};

const container: React.CSSProperties = {
  backgroundColor: "#141417",
  border: "1px solid #2a2a30",
  borderRadius: "8px",
  margin: "0 auto",
  maxWidth: "560px",
  padding: "32px",
};

const eyebrow: React.CSSProperties = {
  color: "#8a8a93",
  fontSize: "11px",
  letterSpacing: "0.18em",
  margin: "0 0 12px",
};

const heading: React.CSSProperties = {
  color: "#ffffff",
  fontSize: "20px",
  fontWeight: 600,
  lineHeight: "1.3",
  margin: 0,
};

const rule: React.CSSProperties = {
  border: "none",
  borderTop: "1px solid #2a2a30",
  margin: "20px 0",
};

const message: React.CSSProperties = {
  color: "#cfcfd6",
  fontSize: "14px",
  lineHeight: "1.6",
  margin: 0,
  whiteSpace: "pre-wrap",
};

const footer: React.CSSProperties = {
  color: "#6c6c75",
  fontSize: "12px",
  margin: 0,
};
