import { Plus } from "lucide-react";
import {
  CONSOLE_APP_SURFACE_RADIUS_CLASS,
  CONSOLE_PRIMARY_BACKGROUND_HEX,
  CONSOLE_PRIMARY_TEXT_HEX,
} from "../runtime/console-layout";

export function PlaceholderInsightPanel() {
  return (
    <div
      className={`relative flex h-full flex-col items-center justify-center overflow-hidden px-8 text-center ${CONSOLE_APP_SURFACE_RADIUS_CLASS}`}
      style={{
        backgroundColor: CONSOLE_PRIMARY_BACKGROUND_HEX,
        color: CONSOLE_PRIMARY_TEXT_HEX,
      }}
    >
      <button
        aria-label="Add a repo to get started"
        className="flex flex-col items-center gap-5 transition hover:opacity-90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-8 focus-visible:outline-[#d5d5d5]"
        type="button"
      >
        <Plus className="size-20 stroke-[1.35]" aria-hidden="true" />
        <span className="max-w-52 text-balance text-2xl font-semibold leading-[1.05] tracking-normal">
          Add a repo to get started
        </span>
      </button>
    </div>
  );
}
