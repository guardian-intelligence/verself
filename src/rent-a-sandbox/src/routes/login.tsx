import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { signIn } from "~/lib/auth";

export const Route = createFileRoute("/login")({
  component: LoginPage,
});

function LoginPage() {
  // Initiate OIDC redirect immediately on mount via loader,
  // but also provide a button as fallback.
  return (
    <div className="flex flex-col items-center justify-center py-24 gap-4">
      <h1 className="text-2xl font-bold">Redirecting to sign in...</h1>
      <p className="text-muted-foreground">
        If you are not redirected,{" "}
        <button onClick={() => signIn()} className="underline text-foreground">
          click here
        </button>
        .
      </p>
      <LoginRedirect />
    </div>
  );
}

function LoginRedirect() {
  if (typeof window !== "undefined") {
    signIn();
  }
  return null;
}
