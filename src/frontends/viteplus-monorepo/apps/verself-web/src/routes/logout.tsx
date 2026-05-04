import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/logout")({
  beforeLoad: () => {
    throw redirect({ href: "/api/v1/auth/logout" });
  },
  component: LogoutPage,
});

function LogoutPage() {
  return (
    <div className="flex items-center justify-center py-24">
      <p className="text-muted-foreground">Signing out...</p>
    </div>
  );
}
