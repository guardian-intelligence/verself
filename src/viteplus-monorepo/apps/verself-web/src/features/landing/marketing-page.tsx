import { Loader2 } from "lucide-react";
import { ANCHORED_SHEET_PRIMARY_ACTION_BUTTON_CLASS } from "@verself/ui/components/ui/anchored-sheet";

import { GitHubIcon } from "./github-icon";
import { VerselfShell } from "./verself-shell";

type MarketingLandingPageProps = {
  readonly onSignInWithEmail?: () => void;
  readonly onSignInWithGitHub?: () => void;
  readonly isGithubLogin?: boolean;
  readonly isEmailLogin?: boolean;
};

export function MarketingLandingPage({
  onSignInWithEmail,
  onSignInWithGitHub,
  isGithubLogin = false,
  isEmailLogin = false,
}: MarketingLandingPageProps) {
  const secondaryActionLabel = isGithubLogin
    ? "Sign in with email instead"
    : isEmailLogin
      ? "Sign in with GitHub"
      : "Sign in with email";
  const secondaryActionHref = isEmailLogin ? "/login" : "/login/email";
  const secondaryActionHandler = isEmailLogin ? onSignInWithGitHub : onSignInWithEmail;
  const secondaryActionTextClasses =
    "mb-3 block w-full max-w-[28rem] self-center px-[1rem] text-center text-sm font-medium text-white/72 hover:text-white transition";

  const primaryActionLabel = isGithubLogin
    ? "Dialing GitHub..."
    : isEmailLogin
      ? "Sign In"
      : "Sign in with GitHub";
  const primaryActionHref = "/login";

  const shouldShowDialingLoader = isGithubLogin;
  const showGitHubLogo = !isEmailLogin;

  const footer = (
    <div
      className="fixed inset-x-0 z-20 flex flex-col items-center gap-2 px-[1rem] transition"
      style={{
        bottom: "calc(max(1rem, env(safe-area-inset-bottom, 0px)) + 1rem)",
      }}
    >
      <a
        href={secondaryActionHref}
        onClick={(event) => {
          if (!secondaryActionHandler) {
            return;
          }
          event.preventDefault();
          secondaryActionHandler();
        }}
        role="button"
        className={secondaryActionTextClasses}
      >
        <span>{secondaryActionLabel}</span>
      </a>

      <a
        href={primaryActionHref}
        onClick={(event) => {
          if (!onSignInWithGitHub) {
            return;
          }
          event.preventDefault();
          onSignInWithGitHub();
        }}
        role="button"
        className={`flex w-full max-w-[28rem] items-center justify-center gap-2 rounded-[var(--anchored-sheet-primary-action-radius,30px)] bg-white text-sm font-semibold text-black shadow-[0_1px_0_rgba(255,255,255,0.45)_inset,0_16px_32px_rgba(0,0,0,0.28)] hover:bg-white/92 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white ${ANCHORED_SHEET_PRIMARY_ACTION_BUTTON_CLASS}`}
      >
        {showGitHubLogo ? <GitHubIcon className="size-4" /> : null}
        <span data-wave-shadow="">{primaryActionLabel}</span>
        {shouldShowDialingLoader ? <Loader2 className="size-4 animate-spin" /> : null}
      </a>
    </div>
  );

  return (
    <VerselfShell footer={footer}>
      <section
        id="intro"
        aria-label="Introducing Verself, CI at Agent-Native Speed"
        className="relative z-10 mx-auto min-h-[100svh] w-[min(86vw,32rem)] [container-type:inline-size]"
      >
        <div className="absolute left-0 right-0 top-[32%] z-10 text-center">
          <p
            className="text-[2.55cqw] font-medium uppercase leading-none text-white/55"
            data-wave-shadow=""
          >
            Introducing
          </p>
          <h1
            className="mx-auto mt-[5.8cqw] max-w-[90cqw] text-[10.2cqw] font-semibold leading-[1.02] text-white"
            data-wave-focus=""
            data-wave-shadow=""
          >
            Verself
          </h1>
          <p
            className="mx-auto mt-[4.5cqw] max-w-[86cqw] text-[3.45cqw] font-medium leading-[1.2] text-white/72"
            data-wave-shadow=""
          >
            CI at Agent-Native Speed
          </p>
          <p
            className="mx-auto mt-[6.5cqw] max-w-[86cqw] text-[3.35cqw] font-medium leading-[1.55] text-white/58"
            data-wave-shadow=""
          >
            Experience up to 20x faster GitHub Actions with our state of the art encrypted artifact
            caching, running on industry-leading bare metal.
          </p>
        </div>
      </section>
    </VerselfShell>
  );
}
