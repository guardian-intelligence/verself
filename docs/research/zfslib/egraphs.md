# E-Graphs

> Core source: Max Willsey, Chandrakana Nandi, Yisu Remy Wang, Oliver Flatt,
> Zachary Tatlock, Pavel Panchekha, "egg: Fast and Extensible Equality Saturation"
>
> PDF: https://arxiv.org/pdf/2004.03082.pdf

## Why this paper matters

If `zfslib` ends up with several equivalent strategies for a state transition, it should not have to
commit to one early. `egg` is useful because it shows how to represent many equivalent plans,
delay expensive repair work, and attach domain facts that are not expressible as pure syntactic
rewrites.

## Passages worth keeping

### 1. Equality saturation side-steps phase ordering by being additive

- p. 1, lines 31-36: equality saturation grows an e-graph by repeatedly applying rewrites; the
  rewrites only add information, which eliminates the need for careful ordering, and extraction picks
  the best term after saturation or timeout.

Why this is the key idea:
- The normal engineering pattern is "pick a strategy first, then hope it was the right one".
- The e-graph pattern is "accumulate equivalent strategies, then choose with a cost model".

### 2. The paper explicitly calls out the ad hoc extension problem

- p. 1, lines 40-44: many applications need domain-specific analyses and end up with ad hoc
  extensions or reimplementations.
- p. 2, lines 15-22: e-class analyses annotate equivalence classes with semilattice facts and let
  rewrites cooperate with those facts.

Why this is useful:
- This is the same failure mode seen in ZFS control code: once raw rewrites are not enough,
  projects start bolting bespoke metadata and filters onto the planner.

### 3. Rebuilding is a concrete performance trick, not just a theorem

- p. 2, lines 8-10: rebuilding defers invariant maintenance to phase boundaries without losing
  soundness.
- p. 8, lines 62-68: chunking and deduplicating the worklist is the reason deferred rebuilding
  improves performance.
- p. 8, lines 71-74: the paper gives a small example where traditional maintenance can require
  `O(n^2)` hashcons updates, while deferred rebuilding needs only `O(n)`.

Why this is unexpectedly practical:
- Many `zfslib` plans will discover multiple equivalent rewrites or multiple facts about the same
  nodes before they need to canonicalize the graph.
- Immediate canonicalization after every tiny change is often the wrong tradeoff.

### 4. E-class analyses are the bridge between syntax and semantics

- p. 2, lines 16-20: facts are introduced, propagated, and joined to maintain an analysis invariant.
- p. 2, lines 34-36: the invariant is what allows rewrites and analyses to cooperate.

Why this matters:
- `zfslib` will likely need semantic facts that pure graph shape does not capture:
  clone counts, "complete snapshot" markers, live leases, retention policy, bandwidth cost,
  namespace pollution risk.

## Direct design implications for `zfslib`

1. Represent equivalent plans explicitly instead of encoding one strategy in control flow.
2. Attach semantic facts to equivalence classes:
   `cost`, `requires_unmount`, `requires_checkpoint`, `preserves_aliases`, `space_delta`.
3. Delay expensive canonicalization until a planning phase boundary.
4. Use a cost model to extract the cheapest legal plan for the current pool state.

## Likely abstractions

```go
type AnalysisFact interface{}

type EquivClass struct {
    Plans []PlanNode
    Facts []AnalysisFact
}

type Extractor interface {
    Best(classes []EquivClass, cost CostModel) (Plan, error)
}
```

The useful import from `egg` is not "use e-graphs everywhere". It is "stop collapsing equivalent
plans too early, and stop mixing rewrite logic with semantic bookkeeping."
