import { createFileRoute, Outlet, Link, redirect } from "@tanstack/react-router";
import { getViewer } from "~/server-fns/auth";

export const Route = createFileRoute("/editor")({
  ssr: "data-only",
  beforeLoad: async ({ location }) => {
    const viewer = await getViewer();
    if (!viewer) {
      throw redirect({
        to: "/login",
        search: { redirect: location.href },
      });
    }
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
