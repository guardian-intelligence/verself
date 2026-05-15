import { Link } from "@tanstack/react-router";

// Chrome for the public legal/policy surface. The product is API/CLI-first, so
// this bar is intentionally sparse: identity, policy index, and a way back to
// the console.
export function PublicTopBar() {
  return (
    <header className="sticky top-0 z-40 border-b border-white/10 bg-black text-white">
      <div className="mx-auto flex h-[var(--header-h)] w-full max-w-[1440px] items-center gap-6 px-4 md:px-6">
        <Link to="/" aria-label="Verself home" className="flex shrink-0 items-center">
          <VerselfWordmark />
        </Link>

        <div className="ml-auto flex items-center gap-2">
          <Link
            to="/policy"
            className="flex h-9 items-center rounded-md px-3 text-sm text-white/60 transition-colors hover:text-white"
          >
            Policy
          </Link>
          <Link
            to="/login"
            search={{ redirect: undefined }}
            className="flex h-9 items-center rounded-md border border-white/15 px-3 text-sm font-medium text-white/90 transition-colors hover:bg-white/10"
          >
            Console
          </Link>
        </div>
      </div>
    </header>
  );
}

function VerselfWordmark() {
  return (
    <span className="inline-flex items-center text-[20px] font-semibold tracking-tight text-white">
      <span aria-hidden="true" className="mr-2 inline-block translate-y-px text-white">
        ▽
      </span>
      <span>Verself</span>
    </span>
  );
}
