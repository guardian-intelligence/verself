import { createFileRoute, Link, redirect } from "@tanstack/react-router";
import { Button } from "@forge-metal/ui/components/ui/button";

export const Route = createFileRoute("/_shell/")({
  beforeLoad: ({ context }) => {
    if (context?.auth?.isAuthenticated) {
      throw redirect({ to: "/executions" });
    }
  },
  component: LandingPage,
  head: () => ({
    meta: [{ title: "Rent-a-Sandbox" }],
  }),
});

function LandingPage() {
  return (
    <div className="py-10 md:py-16">
      <div className="max-w-3xl">
        <h1 className="text-4xl font-semibold leading-tight md:text-5xl">
          GitHub Actions on isolated Firecracker VMs.
        </h1>
        <p className="mt-6 text-lg leading-7 text-muted-foreground">
          Run your existing workflows on Forge Metal runners, then opt into ZFS-backed checkout
          and persistent mounts where they make the job faster.
        </p>
        <div className="mt-8 flex flex-wrap items-center gap-3">
          <Button render={<Link to="/docs">Read the docs</Link>} />
          <Button
            variant="outline"
            render={
              <Link to="/login" search={{ redirect: undefined }}>
                Sign in
              </Link>
            }
          />
        </div>
      </div>
    </div>
  );
}
