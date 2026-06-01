import { Loader2 } from "lucide-react";
import { HandwrittenSignature } from "@verself/brand";

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
}: MarketingLandingPageProps) {
  const emailActionLabel = isGithubLogin ? "Sign in with email instead" : "Sign in with email";
  const githubActionLabel = isGithubLogin ? "Dialing GitHub..." : "Sign in with GitHub";
  const shouldShowDialingLoader = isGithubLogin;

  return (
    <VerselfShell>
      <section
        id="intro"
        aria-label="Introducing Verself, CI at Agent-Native Speed"
        className="relative z-10 mx-auto min-h-[100svh] w-[min(calc(100vw-2rem),48.25rem)] pb-16 pt-[6.7rem]"
      >
        <div className="max-w-[38rem]">
          <h1
            className="text-[48px] font-semibold leading-[1.05] text-black sm:text-[64px]"
            data-wave-focus=""
            data-wave-shadow=""
          >
            Verself
          </h1>
          <p
            className="mt-4 text-[20px] font-medium leading-[1.25] text-black/72"
            data-wave-shadow=""
          >
            CI at Agent-Native Speed
          </p>
        </div>
        <div className="mt-6 flex flex-wrap items-center gap-x-7 gap-y-3 text-sm font-medium leading-none text-black/58">
          <a
            href="/login"
            onClick={(event) => {
              if (!onSignInWithGitHub) {
                return;
              }
              event.preventDefault();
              onSignInWithGitHub();
            }}
            role="button"
            className="inline-flex items-center gap-2 transition hover:text-black focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-black"
          >
            <GitHubIcon className="size-4" />
            <span data-wave-mask="">{githubActionLabel}</span>
            {shouldShowDialingLoader ? <Loader2 className="size-4 animate-spin" /> : null}
          </a>
          <a
            href="/login/email"
            onClick={(event) => {
              if (!onSignInWithEmail) {
                return;
              }
              event.preventDefault();
              onSignInWithEmail();
            }}
            role="button"
            className="inline-flex transition hover:text-black focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-black"
          >
            <span data-wave-mask="">{emailActionLabel}</span>
          </a>
        </div>
        <figure
          aria-label="Shovon Hasan handwritten signature over Verself metal gradient"
          className="relative mt-[47px] aspect-[756/377] w-full overflow-hidden sm:ml-2 sm:w-[calc(100%_-_1rem)] sm:max-w-[47.25rem]"
          style={{ backgroundImage: "var(--verself-landing-metal-gradient)" }}
        >
          <HandwrittenSignature className="absolute left-[11%] top-[11%] h-auto w-[78%] text-black/72 mix-blend-multiply" />
        </figure>
        <p
          className="mt-8 max-w-[34rem] text-[18px] leading-[1.55] text-black/62"
          data-wave-shadow=""
        >
          Start CI from a warm machine with the repo, build graph, artifacts, and agent context
          already in place.
        </p>
      </section>
    </VerselfShell>
  );
}
