import { createFileRoute, Link, redirect } from "@tanstack/react-router";
import { Button } from "@verself/ui/components/ui/button";
import { WhyNotToday } from "~/features/why-not-today";

// Public landing for unauthed visitors. Signed-in users are redirected to
// /executions in beforeLoad — this surface is only reached anonymously.
export const Route = createFileRoute("/_shell/")({
  beforeLoad: ({ context }) => {
    if (context?.auth?.isAuthenticated) {
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
    <section className="relative isolate -mx-4 -my-6 min-h-[calc(100svh-var(--header-h,4rem))] overflow-hidden md:-mx-8 md:-my-8">
      <div className="absolute inset-0 bg-black" />
      <WhyNotToday />
      <div className="relative z-10 flex min-h-[inherit] flex-col items-center justify-center px-6 text-center">
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
          <Button variant="ghost" className="text-white/80 hover:text-white" render={<Link to="/docs">Read the docs</Link>} />
        </div>
      </div>
    </section>
  );
}
