# Sessions And Grades

> Sources:
>
> - Philip Wadler, "Propositions as Sessions"
>   PDF: https://homepages.inf.ed.ac.uk/wadler/papers/propositions-as-sessions/propositions-as-sessions-jfp.pdf
> - Danielle Marshall, Dominic Orchard, "Replicate, Reuse, Repeat: Capturing Non-Linear
>   Communication via Session Types and Graded Modal Types"
>   PDF: https://arxiv.org/pdf/2203.12875.pdf

## Why these papers matter

`zfslib` does not just need a graph. It also needs protocols and capabilities:
who may publish, who may destroy, how many concurrent users a ref may have, when a server-like
resource can spawn fresh linear sessions, and how to make illegal protocol states impossible.

## Passages worth keeping from "Propositions as Sessions"

### 1. The paper makes the concurrency claim unusually explicit

- p. 1, lines 9-16: the abstract says the translation connects session types to linear logic and
  yields a language free from races and deadlock.

Why this matters:
- This is exactly the promise a control library wants from its protocol layer.
- Even if `zfslib` never exposes session syntax, it should want the same property:
  "a destroy/publish/retire handshake cannot get stuck in an impossible half-state".

### 2. Cut elimination as communication is the right metaphor

- p. 2, lines 19-24: the paper states "propositions as session types, proofs as processes, and
  cut elimination as communication".

Why this is foundational:
- It reframes multi-party orchestration as proof reduction rather than ad hoc message passing.
- For `zfslib`, a plan step is not just a function call; it is a typed communication event that
  moves a channel/resource into a new protocol state.

### 3. Restricting communication to one shared channel prevents races

- p. 9, lines 19-28: `Cut` combines parallel composition with name restriction, and the paper says
  that if communication could occur along two channels rather than one, this could lead to races or
  deadlock.
- p. 10, lines 20-25: output allocates a fresh channel and the disjointness of the two processes
  guarantees freedom from races and deadlock.

Why this is unexpectedly practical:
- Fresh names and channel disjointness are not abstract luxuries. They are exactly the shape of
  temporary dataset names, per-operation leases, and per-client work handles.

### 4. Session types evolve as communication proceeds

- p. 10, lines 36-39: the paper says the type of a channel can be regarded as evolving as
  communication proceeds.

Why this is useful:
- `zfslib` can model typestates directly:
  `PendingClone -> Mounted -> Complete -> Published -> Retired`

## Passages worth keeping from "Replicate, Reuse, Repeat"

### 5. Strict linearity is too restrictive for useful systems

- p. 1, lines 16-25: the paper says session types give guarantees, but a strictly linear setting is
  limiting, and graded modal types reintroduce non-linear behaviour in a controlled way.

Why this matters:
- A pure linear story would be too rigid for caches, shared refs, read-only aliases, and bounded
  fanout from one golden image.

### 6. Grades can count exact usage

- p. 2, lines 29-35: the paper says semiring-graded modalities generalize `!`, and the natural
  number modality can count exactly how many times a value may be used.
- p. 3, lines 49-51: overlapping graded assumptions add their grades.

Why this is directly useful:
- This is almost a type-theoretic version of refcounts and lease budgets.
- A snapshot handle of grade `3` is a crisp model for "three clones may still be spawned from
  this ref" or "three reader leases may coexist".

### 7. Reusable channels need protocol restrictions

- p. 6, lines 5-15: the paper says shared channels can become inconsistent if reused after
  multi-step interaction; it avoids that by restricting reusable channels to a single action.

Why this is a strong design warning:
- Not every ZFS operation should be shareable.
- Read-only capabilities may be duplicable; destructive capabilities should probably stay linear.

### 8. Replication can be bounded and typed

- p. 7, lines 11-19: `forkReplicate` turns a server capability usable `0..n` times into a vector of
  client channels, and each client can itself be discarded.
- p. 8, lines 39-45: the paper says graded types capture standard non-linear communication
  patterns on top of the usual linear session presentation.

Why this is novel:
- A bounded spawning interface is a very good fit for job creation from a shared golden image.
- It suggests a library should distinguish:
  - linear capability: destroy, promote, retarget
  - graded capability: spawn up to `n` clones
  - affine capability: may be used zero or one times

## Direct design implications for `zfslib`

1. Give destructive operations linear capabilities.
2. Give read-mostly or spawn-only operations graded capabilities.
3. Encode lifecycle as typestate transitions.
4. Use fresh names/leases when splitting one workflow into parallel sub-workflows.

## Likely abstractions

```go
type Grade uint32

type Capability struct {
    Kind  CapabilityKind
    Grade Grade
}

type State interface {
    PendingClone | Mounted | Complete | Published | Retired
}
```

The important idea is not "session types everywhere". It is "protocol state and resource usage
should be explicit in the API instead of buried in comments and shell retries".
