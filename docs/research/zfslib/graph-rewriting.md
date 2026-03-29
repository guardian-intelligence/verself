# Graph Rewriting

> Core source: Andrea Corradini, Tobias Heindel, Barbara Konig, Dennis Nolte, Arend Rensink,
> "Rewriting Abstract Structures: Materialization Explained Categorically"
>
> PDF: https://arxiv.org/pdf/1902.04809.pdf

## Why this paper matters

This paper is the cleanest bridge between "graph rewriting as category theory" and "we need a
usable implementation story". It is not just about elegance. It says, explicitly, that
materialization and strongest postconditions can be made precise and computable.

## Passages worth keeping

### 1. Materialization is a first-class operation, not an ad hoc trick

- p. 1, lines 13-20: the abstract says the paper develops an over-approximating semantics for
  DPO rewriting, explains materialization via universal properties, and gives a characterization
  of strongest postconditions that is effectively computable under assumptions.
- p. 1, lines 26-30: materialization is tied to shape analysis and also appears in separation
  logic, where it is known as rearrangement.

Why this is interesting:
- This is the theoretical version of a very practical problem: when a summary node or abstract
  state must be "split open" before a rewrite can apply.
- For `zfslib`, that sounds close to "expand abstract intent into a concrete dependency slice"
  before applying destroy, promote, or publish rules.

### 2. The paper says the usual constructions are repeatedly reinvented

- p. 2, lines 5-10: the authors say materialization constructions are "redrawn from scratch for
  every single setting", and that they want to explain them using universal properties.

Why this is useful:
- This matches the local ZFS research almost exactly. Each project rediscovers the dependency
  graph and then hand-builds its own control logic.
- This is strong evidence that `zfslib` should expose materialization-like operations as a real
  library feature, not hide them inside one-off command handlers.

### 3. Strongest postconditions are not just for theorem provers

- p. 2, lines 16-26: the paper claims soundness and completeness for abstract rewriting with
  annotations and explicitly says it can derive strongest postconditions for graph rewriting with
  annotations.

Why this matters:
- If `zfslib` has policies such as `Delete=Defer`, `Retain=3`, or "publish only complete
  snapshots", then the useful question is not just "can I apply this step?" but "what is the most
  precise graph state guaranteed after this step?"
- A strongest-postcondition lens gives a principled way to answer "what tombstones, refs, and
  reachability facts must hold after this rewrite?"

### 4. The gluing condition is the right mental model for partial rewrites

- p. 3, lines 29-35: when the left square can be completed as a pushout, the gluing condition is
  satisfied; in adhesive categories the rewrite result is unique up to canonical isomorphism.

Why this is foundational:
- It gives a crisp "admission test" for destructive operations.
- Inference for `zfslib`: a ZFS error like `EBUSY` can be treated as a failed side condition on a
  graph rewrite, not as an opaque shell error that every caller has to rediscover.

## Direct design implications for `zfslib`

1. Model every destructive or publishing action as a partial graph rewrite with explicit
   preconditions.
2. Make "materialize the relevant dependency slice" a first-class planner step.
3. Attach annotations to nodes and edges:
   `complete`, `lease_count`, `graveyard`, `published_ref`, `created_at`.
4. Expose a strongest-postcondition style API for plans:
   "if this rewrite succeeds, what graph facts are now guaranteed?"

## Likely abstractions

```go
type Graph interface {
    Materialize(match Match) (Subgraph, error)
    Rewrite(rule Rule, match Match) (PostState, error)
}

type Rule struct {
    Name         string
    Preconditions []Predicate
    Effects       []Effect
}
```

The paper does not prescribe this exact API. The inference is that `zfslib` should treat graph
rewriting as the semantic core rather than treating `zfs destroy` and `zfs promote` as the core.
