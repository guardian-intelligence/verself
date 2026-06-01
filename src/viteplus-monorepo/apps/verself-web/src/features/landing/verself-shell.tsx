import { type ReactNode } from "react";
import { cn } from "@verself/ui/lib/utils";
import { recordNaveePointer } from "~/features/navee";

type VerselfShellProps = {
  readonly children: ReactNode;
  readonly footer?: ReactNode;
  readonly navClassName?: string;
  readonly showNav?: boolean;
};

export function VerselfShell({
  children,
  footer,
  navClassName,
  showNav = true,
}: VerselfShellProps) {
  return (
    <main
      className="relative isolate min-h-[100svh] overflow-hidden text-black"
      onPointerDown={recordNaveePointer}
    >
      <div className="relative z-10 min-h-[100svh] w-full">
        {showNav ? <VerselfShellNav className={navClassName} /> : null}
        {children}
        {footer}
      </div>
    </main>
  );
}

function VerselfShellNav({ className }: { readonly className?: string | undefined }) {
  return (
    <div
      className={cn(
        "absolute left-1/2 top-8 z-20 flex w-[min(calc(100vw-2rem),48.25rem)] -translate-x-1/2 items-center justify-between",
        className,
      )}
    >
      <a
        aria-label="Verself"
        className="inline-flex font-semibold leading-none text-black transition hover:text-black/72 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-black"
        data-wave-shadow=""
        href="/"
      >
        verself.sh
      </a>
      <nav
        aria-label="Primary"
        className="flex items-center gap-5 text-sm font-medium leading-none text-black/48"
      >
        <a
          className="transition hover:text-black focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-black"
          href="https://github.com/guardian-intelligence/verself"
        >
          <span data-wave-mask="">Code</span>
        </a>
        <a
          className="transition hover:text-black focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-black"
          href="/docs"
        >
          <span data-wave-mask="">Docs</span>
        </a>
      </nav>
    </div>
  );
}
