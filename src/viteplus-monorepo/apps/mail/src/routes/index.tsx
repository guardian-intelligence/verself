import { createFileRoute, useNavigate, useHydrated } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { getUser, signIn } from "~/lib/auth";
import { keys } from "~/lib/query-keys";
import { useEffect } from "react";

export const Route = createFileRoute("/")({
  component: IndexPage,
});

function IndexPage() {
  const hydrated = useHydrated();
  const navigate = useNavigate();

  const { data: user, isLoading } = useQuery({
    queryKey: keys.user(),
    queryFn: getUser,
    enabled: hydrated,
    staleTime: Infinity,
  });

  // If authenticated, go straight to mail
  useEffect(() => {
    if (user) {
      void navigate({ to: "/mail", replace: true });
    }
  }, [user, navigate]);

  if (!hydrated || isLoading) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        Loading...
      </div>
    );
  }

  if (user) {
    return null; // navigating to /mail
  }

  return (
    <div className="flex items-center justify-center h-full">
      <div className="text-center space-y-4">
        <h1 className="text-2xl font-bold">Webmail</h1>
        <p className="text-muted-foreground">Sign in to access your mailbox.</p>
        <button
          onClick={() => void signIn()}
          className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm"
        >
          Sign in
        </button>
      </div>
    </div>
  );
}
