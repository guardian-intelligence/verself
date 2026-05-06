import { createFileRoute, Link, redirect } from "@tanstack/react-router";
import { Button } from "@verself/ui/components/ui/button";
import { WhyNotToday } from "~/features/why-not-today";
import { getClientAuthSnapshot } from "~/server-fns/auth";

// Top-level marketing landing — deliberately outside _shell so the unauthed
// surface doesn't pick up sidebar/topbar product chrome. Signed-in users are
// redirected straight to the working surface.
export const Route = createFileRoute("/")({
  beforeLoad: async () => {
    const snapshot = await getClientAuthSnapshot();
    if (snapshot.auth.isAuthenticated) {
      throw redirect({ to: "/executions" });
    }
  },
  component: LandingPage,
  head: () => ({
    meta: [{ title: "Verself" }],
  }),
});

function LandingPage() {
  return (
    <main className="relative isolate min-h-svh overflow-hidden bg-black">
      <WhyNotToday />
      <div className="relative z-10 flex min-h-svh flex-col items-center justify-center px-6 text-center">
        <h1
          className="text-5xl font-light leading-[1.05] tracking-tight text-white sm:text-6xl md:text-7xl lg:text-8xl"
          style={{ fontFamily: "Fraunces, serif", fontVariationSettings: "'opsz' 144, 'SOFT' 0" }}
        >
          Why Not Today?
        </h1>
        <div className="mt-12 flex flex-wrap items-center justify-center gap-3">
          <Button
            render={
              <Link to="/login" search={{ redirect: undefined }}>
                Sign in
              </Link>
            }
          />
          <Button
            variant="ghost"
            className="text-white/80 hover:bg-white/10 hover:text-white"
            render={<Link to="/docs">Read the docs</Link>}
          />
        </div>
      </div>
    </main>
  );
}
