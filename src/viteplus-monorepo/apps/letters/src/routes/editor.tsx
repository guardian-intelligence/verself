import { createFileRoute, Outlet, Link, redirect } from "@tanstack/react-router";
import { anonymousAuth, requireAuth } from "@forge-metal/auth-web/isomorphic";

export const Route = createFileRoute("/editor")({
  ssr: "data-only",
  beforeLoad: ({ context, location }) => {
    const auth = requireAuth(context?.auth ?? anonymousAuth, location.href);
    if (!auth.roles.includes("letters_admin")) {
      throw redirect({
        to: "/",
      });
    }
    return { auth };
  },
  component: EditorLayout,
});

function EditorLayout() {
  return (
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
  );
}
