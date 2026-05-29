import { createFileRoute, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import * as React from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";

import { isIamError } from "@verself/sdk/iam-errors";
import {
  AnchoredSheet,
  AnchoredSheetPrimaryActionButton,
  AnchoredSheetPrimaryActionSection,
  type AnchoredSheetRef,
} from "@verself/ui/components/ui/anchored-sheet";
import { Button } from "@verself/ui/components/ui/button";
import { iamErrorMessage } from "~/features/auth/iam-error-copy";
import { GitHubIcon } from "~/features/landing/github-icon";
import { MarketingLandingPage } from "~/features/landing/marketing-page";
import { startGithubLogin } from "~/server-fns/auth";

const loginRoutePathname = "/login";
const loginEmailRoutePathname = "/login/email";

function isLoginRoute(pathname: string): boolean {
  return pathname === loginRoutePathname || pathname === loginEmailRoutePathname;
}

function isEmailLoginRoute(pathname: string): boolean {
  return pathname === loginEmailRoutePathname;
}

export const Route = createFileRoute("/_landing")({
  component: MarketingLandingLayout,
});

function MarketingLandingLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  const sheetRef = React.useRef<AnchoredSheetRef>(null);
  const { pathname } = location;
  const shouldPresentSheet = isLoginRoute(pathname);
  const isEmailLogin = isEmailLoginRoute(pathname);
  const isGithubRoute = pathname === loginRoutePathname;

  // The dial actually leaves the SPA: the server fn sets same-origin login
  // cookies, then we hand the browser to GitHub. isPending stays true through
  // the redirect, so the spinner persists until navigation unloads the page.
  const githubLogin = useMutation({
    mutationFn: async () => {
      const outcome = await startGithubLogin({ data: {} });
      if (isIamError(outcome)) {
        throw outcome;
      }
      return outcome;
    },
    onSuccess: (result) => {
      window.location.assign(result.redirectURL);
    },
  });
  const githubError =
    githubLogin.error && isIamError(githubLogin.error)
      ? iamErrorMessage(githubLogin.error)
      : undefined;

  const openLoginRoute = React.useCallback(() => {
    void navigate({ to: loginRoutePathname });
  }, [navigate]);

  const openLoginWithEmailRoute = React.useCallback(() => {
    void navigate({ to: loginEmailRoutePathname });
  }, [navigate]);

  const dialGithub = githubLogin.mutate;
  const openGitHubSignIn = React.useCallback(() => {
    openLoginRoute();
    dialGithub();
  }, [openLoginRoute, dialGithub]);

  const isGithubLogin = isGithubRoute && githubLogin.isPending;

  const closeLoginRoute = React.useCallback(() => {
    if (location.pathname !== "/") {
      void navigate({ to: "/" });
    }
  }, [location.pathname, navigate]);

  React.useLayoutEffect(() => {
    if (shouldPresentSheet) {
      sheetRef.current?.presentSheet();
      return;
    }
    sheetRef.current?.hideSheet();
  }, [shouldPresentSheet]);

  return (
    <>
      <MarketingLandingPage
        onSignInWithEmail={openLoginWithEmailRoute}
        onSignInWithGitHub={openGitHubSignIn}
        isGithubLogin={isGithubLogin}
        isEmailLogin={isEmailLogin}
      />
      <AnchoredSheet
        ref={sheetRef}
        aria-label="Sign in options"
        defaultPresented={shouldPresentSheet}
        onHide={closeLoginRoute}
        onSheetDismissed={closeLoginRoute}
        onPresent={openLoginRoute}
      >
        <div data-slot="anchored-sheet-content-outer" className="contents">
          <Outlet />
        </div>
        <AnchoredSheetPrimaryActionSection>
          {isEmailLogin ? (
            <Button
              onClick={openGitHubSignIn}
              size="default"
              variant="link"
              className="mb-3 block w-full self-center text-sm font-medium text-foreground/60 hover:text-foreground/80"
            >
              Sign in with GitHub
            </Button>
          ) : (
            <Button
              onClick={openLoginWithEmailRoute}
              size="default"
              variant="link"
              className="mb-3 block w-full self-center text-sm font-medium text-foreground/60 hover:text-foreground/80"
            >
              Sign in with email instead
            </Button>
          )}
          {githubError ? (
            <p role="alert" className="mb-3 text-center text-sm text-destructive">
              {githubError}
            </p>
          ) : null}
          {isEmailLogin ? null : (
            <AnchoredSheetPrimaryActionButton
              onClick={openGitHubSignIn}
              disabled={githubLogin.isPending}
            >
              <span className="flex w-full items-center justify-center gap-2">
                {githubLogin.isPending ? (
                  <>
                    <GitHubIcon className="size-4" />
                    <Loader2 className="size-4 animate-spin" aria-hidden="true" />
                    <span>Dialing GitHub...</span>
                  </>
                ) : (
                  <>
                    <GitHubIcon className="size-4" />
                    <span>Sign in with GitHub</span>
                  </>
                )}
              </span>
            </AnchoredSheetPrimaryActionButton>
          )}
        </AnchoredSheetPrimaryActionSection>
      </AnchoredSheet>
    </>
  );
}
