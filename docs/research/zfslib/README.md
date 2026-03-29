# zfslib Theory Notes

Research notes for a possible `zfslib` control plane: graph rewriting, locality/link structure,
typed protocols, partial-order reduction, and rewrite optimization.

The goal is not to turn ZFS into a theorem. The goal is to identify the smallest theoretical
ideas that explain the recurring implementation problems in the surrounding research:
dependency-aware deletion, metadata/lineage, crash-safe multi-step operations, and concurrent
rewrite scheduling.

## Method

These notes cite exact PDF page/line locations from the linked papers.

- Page/line references are based on local text extraction from the linked PDF.
- They should be treated as "find this passage here" pointers, not archival legal citations.
- Each note focuses on passages that are foundational, surprising, or directly useful for a
  real library: workarounds, constraints, quirks, and places where the theory says something
  operational rather than decorative.

## Notes

| File | Focus |
|------|-------|
| [graph-rewriting.md](graph-rewriting.md) | DPO rewriting, materialization, strongest postconditions |
| [bigraphs.md](bigraphs.md) | Place graphs vs link graphs, sharing, rule priorities, instantaneous rewrites |
| [sessions-and-grades.md](sessions-and-grades.md) | Session types, linearity, bounded non-linearity, replicated servers |
| [exploration.md](exploration.md) | Partial-order reduction, observation equivalence, causal cones |
| [egraphs.md](egraphs.md) | Equality saturation, additive rewrites, deferred invariant repair |

## Working Thesis

The best shape for `zfslib` still looks like:

1. A typed graph model of datasets, snapshots, refs, mounts, leases, and tombstones.
2. A pure planner that rewrites this graph under explicit side conditions.
3. An effectful executor that runs `zfs`, mount, checkpoint, and cleanup actions.
4. A scheduler/tester that understands when two plans are equivalent up to commuting steps.
5. A small optimization layer that can compare equivalent plans instead of hard-coding one.

The papers below are useful because they each illuminate one piece of that stack.
