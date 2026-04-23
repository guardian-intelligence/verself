import { type ComponentType, type ReactNode, useEffect } from "react";
import { Lockup } from "../components/lockup";
import { TREATMENT_WORDMARK_VARIANT, type Treatment } from "./types";
import { useBrandTelemetry } from "./telemetry";

// AppChrome — the single sticky header every Guardian surface renders.
//
// The chrome reads var(--treatment-*) so its ground, wordmark colour, and
// hairline repaint when an ancestor flips data-treatment. The consumer is
// responsible for placing data-treatment on a common ancestor (typically the
// same root div that also wraps <main>), so AppChrome and its siblings
// repaint together.
//
// The wordmark is rendered through a consumer-supplied LinkComponent so
// @forge-metal/brand stays router-agnostic. Apps typically pass TanStack's
// <Link> for SPA navigation; defaults to <a href> if omitted.
//
// On mount (and on treatment change) the chrome emits app_chrome.render to
// the otel pipeline. On wordmark click it emits app_chrome.lockup_click.
// Both spans carry the treatment so every chrome-bearing page can be audited
// in ClickHouse without joining against route metadata.

export interface LinkLikeProps {
  readonly to: string;
  readonly className?: string;
  readonly style?: React.CSSProperties;
  readonly "aria-label"?: string;
  readonly onClick?: React.MouseEventHandler;
  readonly children?: ReactNode;
}

export interface AppChromeProps {
  readonly treatment: Treatment;
  readonly slotRight?: ReactNode;
  readonly wordmarkHref?: string;
  readonly route?: string;
  readonly LinkComponent?: ComponentType<LinkLikeProps>;
}

function DefaultLink({ to, children, ...rest }: LinkLikeProps) {
  return (
    <a href={to} {...rest}>
      {children}
    </a>
  );
}

export function AppChrome({
  treatment,
  slotRight,
  wordmarkHref = "/",
  route,
  LinkComponent = DefaultLink,
}: AppChromeProps) {
  const emitSpan = useBrandTelemetry();
  const variant = TREATMENT_WORDMARK_VARIANT[treatment];

  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("app_chrome.render", {
      route: route ?? window.location.pathname,
      treatment,
      viewport_width: String(window.innerWidth),
      viewport_height: String(window.innerHeight),
      wordmark_variant: variant,
    });
  }, [treatment, route, variant, emitSpan]);

  const handleWordmarkClick = () => {
    if (typeof window === "undefined") return;
    emitSpan("app_chrome.lockup_click", {
      route: route ?? window.location.pathname,
      treatment,
      destination: wordmarkHref,
    });
  };

  return (
    <header
      className="sticky top-0 z-30 transition-colors duration-300 ease-out"
      style={{
        background: "var(--treatment-ground)",
        borderBottom: "1px solid var(--treatment-hairline)",
        color: "var(--treatment-wordmark)",
      }}
    >
      <div className="mx-auto flex h-[var(--header-h)] w-full max-w-7xl items-center justify-between px-4 md:px-6">
        <LinkComponent
          to={wordmarkHref}
          aria-label="Guardian — home"
          className="inline-flex items-center"
          style={{ color: "var(--treatment-wordmark)" }}
          onClick={handleWordmarkClick}
        >
          <Lockup size="sm" variant={variant} title="Guardian" />
        </LinkComponent>
        {slotRight ? <div className="flex items-center gap-4">{slotRight}</div> : null}
      </div>
    </header>
  );
}
