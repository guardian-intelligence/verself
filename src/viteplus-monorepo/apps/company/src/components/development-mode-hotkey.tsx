"use client";

import * as React from "react";

import { DEVELOPMENT_MODE_PARAM_NAME, DEVELOPMENT_MODE_VALUE } from "~/lib/development-mode";

function isEditableElement(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  if (target.isContentEditable) {
    return true;
  }
  const tagName = target.tagName.toLowerCase();
  return tagName === "input" || tagName === "textarea" || tagName === "select";
}

// Cmd+D toggles ?developmentMode=1 site-wide. Mirrors the verself-web hotkey so
// the developer affordance behaves identically across both surfaces.
export function DevelopmentModeHotkey() {
  React.useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!event.metaKey || event.key.toLowerCase() !== "d" || isEditableElement(event.target)) {
        return;
      }

      event.preventDefault();
      const params = new URLSearchParams(window.location.search);
      if (params.has(DEVELOPMENT_MODE_PARAM_NAME)) {
        params.delete(DEVELOPMENT_MODE_PARAM_NAME);
      } else {
        params.set(DEVELOPMENT_MODE_PARAM_NAME, DEVELOPMENT_MODE_VALUE);
      }

      const nextUrl = new URL(window.location.href);
      nextUrl.search = params.toString();
      window.history.replaceState(window.history.state, "", nextUrl);
      window.dispatchEvent(new PopStateEvent("popstate", { state: window.history.state }));
    };

    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
    };
  }, []);

  return null;
}
