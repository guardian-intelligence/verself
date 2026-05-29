import { render } from "@react-email/render";
import { writeFileSync } from "node:fs";
import process from "node:process";
import * as React from "react";

import { AgentNotification } from "./src/agent-notification.ts";

function flag(name: string): string {
  const prefix = `--${name}=`;
  const arg = process.argv.find((value) => value.startsWith(prefix));
  if (arg === undefined) {
    throw new Error(`render-template: missing required flag --${name}`);
  }
  return arg.slice(prefix.length);
}

const mode = flag("mode");
const out = flag("out");

// Prop values are Go text/template actions; email-service substitutes them per
// send. HTML escaping leaves braces/dots untouched, so the rendered output is a
// valid Go template.
const element = React.createElement(AgentNotification, {
  subject: "{{.Subject}}",
  body: "{{.Body}}",
});

const rendered =
  mode === "text"
    ? await render(element, { plainText: true })
    : await render(element, { pretty: false });

writeFileSync(out, rendered);
