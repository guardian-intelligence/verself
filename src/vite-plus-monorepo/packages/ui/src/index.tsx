import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      className={cn("animate-pulse rounded-md bg-primary/10", className)}
      {...props}
    />
  );
}
