# Exploration

> Core source: Marek Chalupa, Krishnendu Chatterjee, Andreas Pavlogiannis, Nishant Sinha,
> Kapil Vaidya, "Data-Centric Dynamic Partial Order Reduction"
>
> PDF: https://arxiv.org/pdf/1610.01188.pdf

## Why this paper matters

As soon as `zfslib` has concurrency, testing and planning become search problems. The key idea in
this paper is that the "obvious" equivalence on interleavings is often too fine, and a more
data-centric equivalence can collapse the search space dramatically.

## Passages worth keeping

### 1. Observation equivalence is coarser than Mazurkiewicz equivalence

- p. 1, lines 14-23: the abstract introduces observation equivalence, says it is data-centric,
  coarser than Mazurkiewicz equivalence, and can be exponentially coarser.
- p. 2, lines 19-25: POR treats interleavings as equivalent when one is obtained from another by
  swapping adjacent independent steps, and still preserves the important safety-style properties.

Why this is foundational:
- The default concurrency story is "explore one trace per independence class".
- The paper says that is sometimes still too expensive because control order is not the real thing
  that matters; observed data dependencies are.

### 2. The motivating example is uncomfortably practical

- p. 3, lines 17-23: two traces are state-equivalent because each read observes the same write,
  even though their event order differs.
- p. 3, lines 32-40: the paper claims observation equivalence is sound, always coarser, and can be
  exponentially more succinct.

Why this matters for `zfslib`:
- Two ZFS plans may differ in internal housekeeping order but produce the same externally
  observable state:
  same published refs, same leases, same surviving snapshots.
- A test runner that explores both plans as distinct may be wasting effort.

### 3. Acyclic cases admit a clean realizability result

- p. 19, lines 28-30: deciding whether a positive annotation is realizable is NP-complete in
  general, but for acyclic architectures the problem is solvable in `O(n^3)`.

Why this is interesting:
- It suggests `zfslib` should try hard to keep key dependency graphs acyclic.
- Once the library permits cyclic protocol dependencies, both reasoning and tooling get harder.

### 4. Causal past cones isolate the events that actually matter

- p. 19, lines 38-45: the causal past cone of an event is the smallest set of prior events that may
  be responsible for enabling it.
- p. 20, lines 39-44: the paper restates the cone operationally and notes it is not just the same as
  "everything that happens before".
- p. 21, lines 2-5: if the same causal past appears in another trace with the same observations, the
  event is inevitable.

Why this is the gem:
- This is a much better rollback/debugging primitive than "all prior steps".
- For `zfslib`, the useful question after a failed publish is: which earlier facts actually enabled
  the publish? That set is likely much smaller than the full execution prefix.

## Direct design implications for `zfslib`

1. Define commutativity and observational equivalence for plans.
2. Test one representative per equivalence class where possible.
3. Track causal cones for emitted side effects such as publish, destroy, and migrate.
4. Prefer acyclic dependency structures in both graph state and protocol state.

## Likely abstractions

```go
type Observation struct {
    PublishedRefs map[string]string
    LeaseTargets  map[string]string
    Tombstones    map[string]bool
}

type TraceClass interface {
    Equivalent(a, b PlanTrace) bool
    CausalCone(t PlanTrace, e EventID) []EventID
}
```

The practical takeaway is simple: a good scheduler/tester for `zfslib` should reason about
observational sameness, not just syntactic trace order.
