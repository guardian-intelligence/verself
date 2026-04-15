import { Toaster as SonnerToaster, toast } from "sonner";

// Thin wrapper so app code imports from @forge-metal/ui alongside every
// other primitive. We theme-opt-out by default — sonner's built-in themes
// assume a dark/light toggle the app shell owns; we instead inherit the
// current foreground/background via CSS variables applied in globals.css.

function Toaster(props: React.ComponentProps<typeof SonnerToaster>) {
  return (
    <SonnerToaster
      position="bottom-right"
      richColors
      closeButton
      toastOptions={{
        classNames: {
          toast:
            "group pointer-events-auto flex w-full items-center gap-3 rounded-md border border-border bg-background p-4 text-sm text-foreground shadow-lg",
          title: "text-sm font-medium text-foreground",
          description: "text-xs text-muted-foreground",
        },
      }}
      {...props}
    />
  );
}

export { Toaster, toast };
