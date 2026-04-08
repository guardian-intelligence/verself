import { createFileRoute, Outlet, Link, useNavigate, ClientOnly } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { getUser, signIn } from "~/lib/auth";
import { keys } from "~/lib/query-keys";

export const Route = createFileRoute("/editor")({
  component: EditorLayout,
});

function EditorLayout() {
  return (
    <ClientOnly fallback={<div className="py-24 text-center text-muted-foreground">Loading...</div>}>
      <AuthGate>
        <div className="max-w-4xl mx-auto px-6 py-8">
          <div className="flex items-center gap-4 mb-8 border-b border-border pb-4">
            <Link
              to="/editor"
              className="text-sm text-muted-foreground hover:text-foreground [&.active]:text-foreground [&.active]:font-medium"
            >
              All Posts
            </Link>
            <Link
              to="/editor/new"
              className="text-sm px-3 py-1.5 rounded-md bg-foreground text-background hover:bg-foreground/90"
            >
              New Post
            </Link>
          </div>
          <Outlet />
        </div>
      </AuthGate>
    </ClientOnly>
  );
}

function AuthGate({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();
  const { data: user, isLoading } = useQuery({
    queryKey: keys.user(),
    queryFn: getUser,
    staleTime: Infinity,
    refetchOnWindowFocus: true,
  });

  if (isLoading) {
    return <div className="py-24 text-center text-muted-foreground">Checking authentication...</div>;
  }

  if (!user) {
    return (
      <div className="py-24 text-center">
        <h1 className="text-2xl font-bold mb-4">Sign in required</h1>
        <p className="text-muted-foreground mb-6">You need to sign in to access the editor.</p>
        <button
          onClick={() => void signIn()}
          className="px-4 py-2 rounded-md bg-foreground text-background hover:bg-foreground/90"
        >
          Sign in
        </button>
      </div>
    );
  }

  return <>{children}</>;
}
