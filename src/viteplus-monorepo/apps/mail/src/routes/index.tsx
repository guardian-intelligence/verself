import { createFileRoute, redirect } from "@tanstack/react-router";
import { getViewer } from "~/server-fns/auth";

export const Route = createFileRoute("/")({
  beforeLoad: async () => {
    const viewer = await getViewer();
    if (viewer) {
      throw redirect({ to: "/mail" });
    }
  },
  component: IndexPage,
});

function IndexPage() {
  return (
    <div className="flex items-center justify-center h-full">
      <div className="text-center space-y-4">
        <h1 className="text-2xl font-bold">Webmail</h1>
        <p className="text-muted-foreground">Sign in to access your mailbox.</p>
        <a href="/login" className="inline-flex px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm">
          Sign in
        </a>
      </div>
    </div>
  );
}
