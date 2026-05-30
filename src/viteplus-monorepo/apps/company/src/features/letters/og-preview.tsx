"use client";

import * as React from "react";
import { ogSpecFor } from "~/og/catalog";
import { buildOGCard } from "~/og/template";
import { isDevelopmentModeEnabled } from "~/lib/development-mode";

const OG_PREVIEW_PARAM = "og";

function isEditableElement(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName.toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select";
}

// "s" toggles ?og=1 on a letter post, but only while developer mode is on.
// Same URL-as-state mechanism as the developer-mode hotkey, so a refresh keeps
// the toggle and the router re-renders off the search param.
export function LetterOgPreviewHotkey() {
  React.useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (
        event.key.toLowerCase() !== "s" ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey ||
        isEditableElement(event.target)
      ) {
        return;
      }
      if (!isDevelopmentModeEnabled()) return;

      event.preventDefault();
      const params = new URLSearchParams(window.location.search);
      if (params.get(OG_PREVIEW_PARAM) === "1") {
        params.delete(OG_PREVIEW_PARAM);
      } else {
        params.set(OG_PREVIEW_PARAM, "1");
      }
      const nextUrl = new URL(window.location.href);
      nextUrl.search = params.toString();
      window.history.replaceState(window.history.state, "", nextUrl);
      window.dispatchEvent(new PopStateEvent("popstate", { state: window.history.state }));
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  return null;
}

function PreviewFrame({
  label,
  children,
}: {
  readonly label: string;
  readonly children: React.ReactNode;
}) {
  return (
    <figure className="m-0">
      <figcaption className="mb-3 font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-[var(--treatment-muted-meta)]">
        {label}
      </figcaption>
      <div className="overflow-hidden rounded-md border border-[var(--treatment-surface-border)] bg-white shadow-sm">
        {children}
      </div>
    </figure>
  );
}

// The developer view: the post's OG card as both the rasterised PNG (what
// crawlers embed) and the source SVG it is rendered from, built client-side
// from the same spec the server uses. No article body — just the two cards.
export function LetterOgPreview({ slug }: { readonly slug: string }) {
  const spec = ogSpecFor(`letter/${slug}`);
  const built = spec ? buildOGCard(spec) : null;
  const svg = built && built.ok ? built.svg : null;
  const svgSrc = svg ? `data:image/svg+xml;utf8,${encodeURIComponent(svg)}` : null;

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-10 px-[var(--chrome-inline-gap)] py-12">
      <p className="m-0 font-mono text-[11px] uppercase tracking-[0.16em] text-[var(--treatment-muted-faint)]">
        OG preview · /og/letter/{slug} · press “s” to exit
      </p>
      <PreviewFrame label="PNG · rasterised (what social embeds)">
        <img
          src={`/og/letter/${slug}`}
          alt={`OG PNG for ${slug}`}
          width={1200}
          height={630}
          className="block w-full"
        />
      </PreviewFrame>
      <PreviewFrame label="SVG · source">
        {svgSrc ? (
          <img
            src={svgSrc}
            alt={`OG SVG for ${slug}`}
            width={1200}
            height={630}
            className="block w-full"
          />
        ) : (
          <p className="m-0 p-6 font-mono text-[12px] text-[var(--treatment-muted-meta)]">
            no OG spec for this slug
          </p>
        )}
      </PreviewFrame>
    </div>
  );
}
