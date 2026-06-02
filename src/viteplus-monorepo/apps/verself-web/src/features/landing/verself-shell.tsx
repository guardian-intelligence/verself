import { type ReactNode } from "react";
import { Lockup } from "@verself/brand";
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
      className="relative isolate min-h-[100svh] overflow-hidden text-white"
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
        "absolute left-1/2 top-[12%] z-20 flex w-[min(86vw,32rem)] -translate-x-1/2 items-center justify-between [container-type:inline-size]",
        className,
      )}
    >
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
  );
}
