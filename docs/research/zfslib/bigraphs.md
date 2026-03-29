# Bigraphs

> Core source: Blair Archibald, Muffy Calder, Michele Sevegnani,
> "Practical Modelling with Bigraphs"
>
> PDF: https://arxiv.org/pdf/2405.20745.pdf

## Why this paper matters

A plain dataset DAG is not the whole problem. The system also has locality, containment,
mounting, links, and control rules. This paper is useful because it treats locality and linking
as separate relations and then spends real time on pragmatic extensions: sharing, priorities,
and instantaneous rewrite classes.

## Passages worth keeping

### 1. Bigraphs separate locality from connectivity

- p. 1, lines 28-33: entities can relate spatially through nesting and through linking;
  place graphs are a forest, link graphs are a hypergraph, and a bigraph reactive system
  evolves via rewrite rules.

Why this is foundational:
- A ZFS library usually starts with one graph: origin and clone edges.
- In practice, `zfslib` needs at least two:
  - containment/locality: pool -> dataset -> snapshot, namespace -> mount tree
  - linkage/dependency: origin, aliases, leases, worker attachments, replication refs

### 2. Sharing turns the place graph from a forest into a DAG

- p. 6, lines 48-54: standard bigraphs give each entity one parent; bigraphs with sharing relax
  the place graph from a forest to a directed acyclic graph.
- p. 6, lines 55-60: the paper treats sharing as a simple modelling extension even though it has
  important theoretical consequences.

Why this is unexpected:
- The step from "tree of datasets" to "DAG with shared entities" is exactly where many real
  systems get awkward.
- If `zfslib` models views, aliases, or attached workspaces that can appear in more than one
  logical scope, a DAG is the right default, not a tree with hacks.

### 3. "Solid" rewrite sides are required for unique occurrences

- p. 11, lines 61-62: in practice, the left-hand side of a rewrite is required to be solid; the
  note says this ensures unique occurrences and is central to probabilistic and stochastic rewriting.

Why this is useful:
- Matching ambiguity is not cosmetic. It affects how many executions a scheduler thinks are
  distinct.
- For `zfslib`, this suggests that operational rules should prefer canonical, uniquely matched
  patterns when possible, especially for cleanup and GC passes.

### 4. Priorities are a real modelling tool, not a hack

- p. 15, lines 35-40: the paper introduces a concrete leak scenario where a more general movement
  rule should not run before a more specific cleanup rule.
- p. 15, lines 45-50: BigraphER groups rules into priority classes; lower-priority rules are only
  checked when higher-priority rules do not match.
- p. 15, lines 52-54: the paper explicitly warns that priorities can suppress useful general-case
  matches elsewhere.

Why this matters:
- `zfslib` will likely need "repair before publish" and "detach before destroy" phases.
- The warning is important: priorities help, but they also create hidden starvation and missed
  opportunities if overused.

### 5. Instantaneous classes collapse spurious intermediate states

- p. 16, lines 31-39: instantaneous rules fully reduce before any additional rules are called,
  must be confluent, and remove many spurious states and interleavings.

Why this is strong guidance:
- Some ZFS plans have obvious housekeeping steps that should not be externally visible:
  `rename -> clear alias -> retarget ref -> drop tombstone marker`.
- If those steps are confluent and internal, the state space should not expose each micro-step.

## Direct design implications for `zfslib`

1. Use at least a two-relation model:
   containment/place and dependency/link.
2. Permit DAG locality when modelling aliases, shared views, or overlapping ownership.
3. Define internal rewrite classes that must reach a local fixpoint before external observation.
4. Treat priorities as an explicit scheduler feature, with warnings and tests around starvation.

## Likely abstractions

```go
type PlaceEdge struct{ Parent, Child NodeID }
type LinkEdge struct{ Kind LinkKind; From, To NodeID }

type RuleClass struct {
    Name          string
    Priority      int
    Instantaneous bool
    Confluent     bool
}
```

The bigraph insight is not "use bigraph syntax". It is "model locality and dependency separately,
or the library will eventually smuggle one into the other and become hard to reason about."
