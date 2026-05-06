import { createFileRoute, Link, redirect } from "@tanstack/react-router";
import { LandingTopBar } from "~/features/landing/landing-top-bar";
import { LandingScene } from "~/features/scene3d";
import { resolveDefaultSignedInPath } from "~/features/shell/org-route-loaders";
import { getClientAuthSnapshot } from "~/server-fns/auth";

// Top-level marketing landing — deliberately outside _shell so the unauthed
// surface doesn't pick up sidebar/topbar product chrome. Signed-in users are
// redirected straight to the working surface.
export const Route = createFileRoute("/")({
  beforeLoad: async ({ context }) => {
    const snapshot = await getClientAuthSnapshot();
    if (snapshot.auth.isAuthenticated) {
      throw redirect({
        href: await resolveDefaultSignedInPath(context.queryClient, snapshot.auth),
        replace: true,
      });
    }
  },
  component: LandingPage,
  head: () => ({
    meta: [{ title: "Verself" }],
  }),
});

function LandingPage() {
  return (
    <main className="relative isolate min-h-svh overflow-hidden bg-black text-white">
      <LandingScene />
      <LandingTopBar />

      <div className="relative z-10 mx-auto flex min-h-svh max-w-3xl flex-col items-center px-6 pt-28 text-center md:pt-36">
        <h1
          className="text-5xl font-light leading-[1.05] tracking-tight text-white sm:text-6xl md:text-7xl"
          style={{ fontFamily: "Fraunces, serif", fontVariationSettings: "'opsz' 144, 'SOFT' 0" }}
        >
          It&rsquo;s Better on Metal
        </h1>
        <p className="mt-6 max-w-xl text-base text-white/60 md:text-lg">
          Sandbox compute on Firecracker. Faster CI today; persistent dev VMs and Lambda-style
          workloads on the same isolation substrate.
        </p>
        <div className="mt-10 flex flex-wrap items-center justify-center gap-3">
          <Link
            to="/login"
            search={{ redirect: undefined }}
            className="rounded-md bg-white px-4 py-2 text-sm font-medium text-black transition-colors hover:bg-white/90"
          >
            Get started
          </Link>
          <Link
            to="/docs"
            className="rounded-md border border-white/20 px-4 py-2 text-sm font-medium text-white/90 transition-colors hover:bg-white/10"
          >
            Read the docs
          </Link>
        </div>
      </div>
    </main>
  );
}
