import { type ComponentType, type ReactNode, useEffect } from "react";
import { Lockup } from "../components/lockup";
import { TREATMENT_DEFAULT_SECTION, TREATMENT_WORDMARK_VARIANT, type Treatment } from "./types";
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
// @verself/brand stays router-agnostic. Apps typically pass TanStack's
// <Link> for SPA navigation; defaults to <a href> if omitted.
//
// On mount (and on treatment change) the chrome emits app_chrome.render to
// the otel pipeline. On wordmark click it emits app_chrome.lockup_click.
// Both spans carry the treatment and the resolved section label so every
// chrome-bearing page can be audited in ClickHouse without joining against
// route metadata — the cutover to GUARDIAN · {SECTION} is verifiable from
// telemetry alone.

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
  // Section suffix shown after `GUARDIAN · ` in the masthead. `null` forces
  // the bare lockup (no suffix) even if the treatment has a default — useful
  // for the house root on the Workshop treatment, or for an editorial
  // cover page that wants the full masthead without a section tag.
  // `undefined` (the default) resolves to the treatment's default section.
  readonly section?: string | null;
  // Whether to draw the chrome's bottom rule. Default true. Layouts may set
  // false on routes where the page already provides its own separation
  // (e.g. /newsroom/$slug, where the article header is the masthead).
  readonly bottomRule?: boolean;
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
  section,
  bottomRule = true,
}: AppChromeProps) {
  const emitSpan = useBrandTelemetry();
  const variant = TREATMENT_WORDMARK_VARIANT[treatment];
  const resolvedSection =
    section === null ? undefined : (section ?? TREATMENT_DEFAULT_SECTION[treatment]);

  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("app_chrome.render", {
      route: route ?? window.location.pathname,
      treatment,
      viewport_width: String(window.innerWidth),
      viewport_height: String(window.innerHeight),
      wordmark_variant: variant,
      // Face and section close the loop on the Geist cutover: a query that
      // groups by (route, wordmark_face) confirms the masthead landed on
      // every surface without relying on DOM scraping.
      wordmark_face: "geist-uppercase",
      section: resolvedSection ?? "",
    });
  }, [treatment, route, variant, resolvedSection, emitSpan]);

  const handleWordmarkClick = () => {
    if (typeof window === "undefined") return;
    emitSpan("app_chrome.lockup_click", {
      route: route ?? window.location.pathname,
      treatment,
      destination: wordmarkHref,
      section: resolvedSection ?? "",
    });
  };

  // The chrome reads as one bar across all three treatments: same width,
  // same items, same placement. Each treatment paints itself via the
  // var(--treatment-*) ramp, but the geometry stays uniform so the masthead
  // does not change shape when a reader crosses /letters → /newsroom → /.
  // Consumers align their content wrappers to the same max-w-6xl so the
  // wordmark and the page body sit on the same column. The rule is drawn
  // on the inner container, never on the <header> element, so it cannot
  // reach the viewport edge.
  return (
    <header
      className="sticky top-0 z-30 transition-colors duration-300 ease-out"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-wordmark)",
      }}
    >
      <div className="mx-auto w-full max-w-6xl px-4 md:px-6">
        <div
          className="flex h-[var(--header-h)] items-center justify-between"
          style={{
            borderBottom: bottomRule
              ? "var(--treatment-rule-thickness) solid var(--treatment-rule-color)"
              : undefined,
          }}
        >
          <LinkComponent
            to={wordmarkHref}
            aria-label={resolvedSection ? `Guardian — ${resolvedSection} — home` : "Guardian — home"}
            className="inline-flex items-center"
            style={{ color: "var(--treatment-wordmark)" }}
            onClick={handleWordmarkClick}
          >
            <Lockup
              size="sm"
              variant={variant}
              title="Guardian"
              {...(resolvedSection ? { section: resolvedSection } : {})}
            />
          </LinkComponent>
          {slotRight ? <div className="flex items-center gap-4">{slotRight}</div> : null}
        </div>
      </div>
    </header>
  );
}
