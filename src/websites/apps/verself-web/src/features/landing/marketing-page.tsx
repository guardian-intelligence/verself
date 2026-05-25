import { Lockup } from "@verself/brand";

import { DotMatrixField, recordDotMatrixClick } from "./dot-matrix";

export function MarketingLandingPage() {
  return (
    <main
      className="relative isolate flex min-h-[100svh] justify-center overflow-hidden bg-[#080909] text-white"
      onPointerDown={recordDotMatrixClick}
    >
      <div className="fixed inset-0 -z-20">
        <DotMatrixField className="opacity-95" />
      </div>
      <div
        aria-hidden="true"
        className="fixed inset-0 -z-10 bg-[radial-gradient(circle_at_50%_45%,rgba(255,255,255,0.06),transparent_25%),radial-gradient(circle_at_66%_40%,rgba(255,255,255,0.05),transparent_18%)]"
      />

      <section
        id="intro"
        aria-label="Introducing Verself, CI at Agent-Native Speed"
        className="relative z-10 min-h-[100svh] w-[min(86vw,32rem)] [container-type:inline-size]"
      >
        <div className="absolute left-0 right-0 top-[12%] z-10 flex items-center justify-between">
          <a
            aria-label="Guardian"
            className="inline-flex text-white transition hover:text-white/78 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
            data-wave-shadow=""
            href="https://guardianintelligence.org"
          >
            <Lockup size="sm" title="Guardian" style={{ padding: 0 }} />
          </a>
          <nav
            aria-label="Primary"
            className="flex items-center gap-[clamp(1rem,4.2cqw,1.35rem)] text-[clamp(0.75rem,2.25cqw,0.875rem)] font-medium leading-none text-white/62"
          >
            <a
              className="transition hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
              href="https://github.com/guardian-intelligence/verself"
            >
              <span data-wave-mask="">Code</span>
            </a>
            <a
              className="transition hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
              href="/docs"
            >
              <span data-wave-mask="">Docs</span>
            </a>
          </nav>
        </div>

        <div className="absolute left-0 right-0 top-[32%] z-10 text-center">
          <p
            className="text-[2.55cqw] font-medium uppercase leading-none text-white/55"
            data-wave-shadow=""
          >
            Introducing
          </p>
          <h1
            className="mx-auto mt-[5.8cqw] max-w-[90cqw] text-[10.2cqw] font-semibold leading-[1.02] text-white"
            data-wave-focus=""
            data-wave-shadow=""
          >
            Verself
          </h1>
          <p
            className="mx-auto mt-[4.5cqw] max-w-[86cqw] text-[3.45cqw] font-medium leading-[1.2] text-white/72"
            data-wave-shadow=""
          >
            CI at Agent-Native Speed
          </p>
          <p
            className="mx-auto mt-[6.5cqw] max-w-[86cqw] text-[3.35cqw] font-medium leading-[1.55] text-white/58"
            data-wave-shadow=""
          >
            Experience up to 20x faster GitHub Actions with our state of the art encrypted artifact
            caching, running on industry-leading bare metal.
          </p>
        </div>

        <div className="absolute bottom-[7.2%] left-0 right-0 z-10">
          <a
            className="mx-auto flex h-[12cqw] w-[81%] items-center justify-center rounded-full bg-white text-[3.2cqw] font-semibold text-black shadow-[0_1px_0_rgba(255,255,255,0.45)_inset,0_16px_32px_rgba(0,0,0,0.28)] transition hover:bg-white/92 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
            data-wave-shadow=""
            href="/login"
          >
            <span>Get Verself</span>
          </a>
        </div>
        <a
          className="absolute bottom-[3.2%] left-0 right-0 z-10 text-center text-[3cqw] font-medium text-white/42 transition hover:text-white/68 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
          data-wave-shadow=""
          href="#intro"
        >
          <span>Learn more about Verself&nbsp;›</span>
        </a>
      </section>
    </main>
  );
}
