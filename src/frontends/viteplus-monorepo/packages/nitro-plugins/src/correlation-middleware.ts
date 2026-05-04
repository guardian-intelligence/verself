import { randomUUID } from "node:crypto";
import { defineEventHandler, getCookie, setCookie, type H3Event } from "nitro/h3";

import { correlationContextKey, correlationCookieName } from "./correlation.ts";

export const correlationMiddleware = defineEventHandler((event: H3Event) => {
  let correlationID = (getCookie(event, correlationCookieName) ?? "").trim();
  if (correlationID === "") {
    correlationID = randomUUID();
    // Correlation is not an auth secret. It stays same-origin and readable so
    // authFetch can forward it only to /api/v1/* without touching Zitadel.
    setCookie(event, correlationCookieName, correlationID, {
      path: "/",
      sameSite: "lax",
      secure: true,
      httpOnly: false,
      maxAge: 60 * 60 * 24 * 30,
    });
  }

  (event.context as Record<string, unknown>)[correlationContextKey] = correlationID;
});
