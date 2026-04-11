import { createFileRoute, redirect } from "@tanstack/react-router";
import { anonymousAuth } from "@forge-metal/auth-web/isomorphic";

export const Route = createFileRoute("/")({
  beforeLoad: ({ context }) => {
    const auth = context?.auth ?? anonymousAuth;
    if (auth.isAuthenticated) {
      throw redirect({ to: "/mail" });
    }
  },
  component: IndexPage,
});

function IndexPage() {
  return (
    <div className="flex items-center justify-center h-full">
      <div className="text-center space-y-4 max-w-sm px-4">
        <div className="mx-auto w-16 h-16 rounded-2xl bg-primary/10 flex items-center justify-center">
          <svg
            viewBox="0 0 24 24"
            className="w-8 h-8 text-primary"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
          >
            <path d="M21.75 6.75v10.5a2.25 2.25 0 0 1-2.25 2.25h-15a2.25 2.25 0 0 1-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25m19.5 0v.243a2.25 2.25 0 0 1-1.07 1.916l-7.5 4.615a2.25 2.25 0 0 1-2.36 0L3.32 8.91a2.25 2.25 0 0 1-1.07-1.916V6.75" />
          </svg>
        </div>
        <h1 className="text-2xl font-bold">Webmail</h1>
        <p className="text-muted-foreground text-sm">Sign in to access your mailbox.</p>
        <a
          href="/login"
          className="inline-flex items-center gap-2 px-5 py-2.5 rounded-lg bg-primary text-primary-foreground hover:opacity-90 text-sm font-medium transition-opacity"
        >
          Sign in
        </a>
      </div>
    </div>
  );
}
