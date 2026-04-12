// Backward-compatible barrel: existing call sites do `import { cn, Skeleton }
// from "@forge-metal/ui"`. Newer code should prefer the canonical shadcn
// import paths — `@forge-metal/ui/lib/utils` for cn and
// `@forge-metal/ui/components/ui/skeleton` for Skeleton — so the bundle can
// tree-shake unused primitives and CLI add commands stay round-trippable.
export { cn } from "./lib/utils";
export { Skeleton } from "./components/ui/skeleton";
