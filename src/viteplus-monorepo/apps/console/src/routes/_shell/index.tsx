import { createFileRoute, getRouteApi, Link, redirect } from "@tanstack/react-router";
import { Button } from "@verself/ui/components/ui/button";

// Parent _shell route already loads the platform docs origin; reuse its
// loader data so we don't double-fetch it per landing render.
const shellRouteApi = getRouteApi("/_shell");

export const Route = createFileRoute("/_shell/")({
  beforeLoad: ({ context }) => {
    if (context?.auth?.isAuthenticated) {
      throw redirect({ to: "/executions" });
    }
  },
  component: LandingPage,
  head: () => ({
    meta: [{ title: "Console" }],
  }),
});

function LandingPage() {
  const platformOrigin = shellRouteApi.useLoaderData();
  return (
    <div className="py-10 md:py-16">
      <div className="max-w-3xl">
        <h1 className="text-4xl font-semibold leading-tight md:text-5xl">
          GitHub Actions on isolated Firecracker VMs.
        </h1>
        <p className="mt-6 text-lg leading-7 text-muted-foreground">
          Run your existing workflows on Verself runners, then opt into ZFS-backed checkout and
          persistent mounts where they make the job faster.
        </p>
        <div className="mt-8 flex flex-wrap items-center gap-3">
          <Button
            render={
              <a
                href={`${platformOrigin.replace(/\/$/, "")}/docs`}
                target="_blank"
                rel="noopener noreferrer"
              >
                Read the docs
              </a>
            }
          />
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
