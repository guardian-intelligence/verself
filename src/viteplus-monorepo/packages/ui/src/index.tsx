// Backward-compatible barrel: existing call sites do `import { cn, Skeleton }
// from "@verself/ui"`. Newer code should prefer the canonical shadcn
// import paths — `@verself/ui/lib/utils` for cn and
// `@verself/ui/components/ui/skeleton` for Skeleton — so the bundle can
// tree-shake unused primitives and CLI add commands stay round-trippable.
export { cn } from "./lib/utils";
export { Skeleton } from "./components/ui/skeleton";
