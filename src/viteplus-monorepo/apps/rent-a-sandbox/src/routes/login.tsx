import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
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
        <button onClick={() => void signIn()} className="underline text-foreground">
          click here
        </button>
        .
      </p>
      <LoginRedirect />
    </div>
  );
}

function LoginRedirect() {
  useQuery({
    queryKey: ["auth", "login"],
    queryFn: async () => {
      await signIn();
      return true;
    },
    retry: false,
    staleTime: Infinity,
    enabled: typeof window !== "undefined",
  });
  return null;
}
