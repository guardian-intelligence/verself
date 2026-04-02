# Style

## The Essence Of Style

> “There are three things extremely hard: steel, a diamond, and to know one's self.” — Benjamin
> Franklin

Our coding style is evolving. A give-and-take at the intersection of engineering and art. Numbers
and human intuition. Reason and experience. First principles and knowledge. Precision and poetry.
Just like music. A tight beat. A rare groove. Words that rhyme and rhymes that break. Biodigital
jazz. This is what we've learned along the way. The best is yet to come.

_This guide is adapted from [TigerBeetle's TIGER_STYLE.md](https://github.com/tigerbeetle/tigerbeetle/blob/main/docs/TIGER_STYLE.md),
the most rigorous Zig coding standard in existence. Where we diverge, we note it._

_A full shallow clone of the TigerBeetle repo lives at `docs/references/tigerbeetle/`. When
working on homestead-smelter, grep through that codebase (`rg -t zig <pattern> docs/references/tigerbeetle/src/`)
to understand how a production, high-performance, security-critical Zig project structures its code —
allocation patterns, assertion discipline, error handling, comptime usage, and module boundaries._

## Why Have Style?

Another word for style is design.

> “The design is not just what it looks like and feels like. The design is how it works.” — Steve
> Jobs

Our design goals are safety, performance, and developer experience. In that order. All three are
important. Good style advances these goals. Does the code make for more or less safety, performance
or developer experience? That is why we need style.

Put this way, style is more than readability, and readability is table stakes, a means to an end
rather than an end in itself.

> “...in programming, style is not something to pursue directly. Style is necessary only where
> understanding is missing.” ─ [Let Over
> Lambda](https://letoverlambda.com/index.cl/guest/chap1.html)

This document explores how we apply these design goals to coding style. First, a word on simplicity,
elegance and technical debt.

## On Simplicity And Elegance

Simplicity is not a free pass. It's not in conflict with our design goals. It need not be a
concession or a compromise.

Rather, simplicity is how we bring our design goals together, how we identify the “super idea” that
solves the axes simultaneously, to achieve something elegant.

> “Simplicity and elegance are unpopular because they require hard work and discipline to achieve” —
> Edsger Dijkstra

Contrary to popular belief, simplicity is also not the first attempt but the hardest revision. It's
easy to say “let's do something simple”, but to do that in practice takes thought, multiple passes,
many sketches, and still we may have to [“throw one
away”](https://en.wikipedia.org/wiki/The_Mythical_Man-Month).

The hardest part, then, is how much thought goes into everything.

We spend this mental energy upfront, proactively rather than reactively, because we know that when
the thinking is done, what is spent on the design will be dwarfed by the implementation and testing,
and then again by the costs of operation and maintenance.

An hour or day of design is worth weeks or months in production:

> “the simple and elegant systems tend to be easier and faster to design and get right, more
> efficient in execution, and much more reliable” — Edsger Dijkstra

## Technical Debt

What could go wrong? What's wrong? Which question would we rather ask? The former, because code,
like steel, is less expensive to change while it's hot. A problem solved in production is many times
more expensive than a problem solved in implementation, or a problem solved in design.

Since it's hard enough to discover showstoppers, when we do find them, we solve them. We don't allow
potential memcpy latency spikes, or exponential complexity algorithms to slip through.

> “You shall not pass!” — Gandalf

In other words, we maintain a “zero technical debt” policy. We do it right the first time. This is
important because the second time may not transpire, and because doing good work, that we can be
proud of, builds momentum.

We know that what we ship is solid. We may lack crucial features, but what we have meets our design
goals. This is the only way to make steady incremental progress, knowing that the progress we have
made is indeed progress.

## Safety

> “The rules act like the seat-belt in your car: initially they are perhaps a little uncomfortable,
> but after a while their use becomes second-nature and not using them becomes unimaginable.” —
> Gerard J. Holzmann

[NASA's Power of Ten — Rules for Developing Safety Critical
Code](https://spinroot.com/gerard/pdf/P10.pdf) will change the way you code forever. To expand:

- Use **only very simple, explicit control flow** for clarity. **Do not use recursion** to ensure
  that all executions that should be bounded are bounded. Use **only a minimum of excellent
  abstractions** but only if they make the best sense of the domain. Abstractions are [never zero
  cost](https://isaacfreund.com/blog/2022-05/). Every abstraction introduces the risk of a leaky
  abstraction.

- **Put a limit on everything** because, in reality, this is what we expect—everything has a limit.
  For example, all loops and all queues must have a fixed upper bound to prevent infinite loops or
  tail latency spikes. This follows the [“fail-fast”](https://en.wikipedia.org/wiki/Fail-fast)
  principle so that violations are detected sooner rather than later. Where a loop cannot terminate
  (e.g. an event loop), this must be asserted.

- Use explicitly-sized types (`u32`, `u64`, `u16`) for all **wire-format fields** — struct fields
  that are serialized to the binary protocol, written to disk, or sent over vsock. This guarantees
  identical layout across architectures. Use `usize` freely for slice lengths, buffer positions,
  loop indices, and other values that are internal to the process and never cross a trust boundary.
  The wire protocol structs in `root.zig` (`HelloFrame`, `SampleFrame`) must never contain `usize`.

- **Assertions detect programmer errors. Unlike operating errors, which are expected and which must
  be handled, assertion failures are unexpected. The only correct way to handle corrupt code is to
  crash. Assertions downgrade catastrophic correctness bugs into liveness bugs. Assertions are a
  force multiplier for discovering bugs by fuzzing.**

  - **Assert all function arguments and return values, pre/postconditions and invariants.** A
    function must not operate blindly on data it has not checked. The purpose of a function is to
    increase the probability that a program is correct. Assertions within a function are part of how
    functions serve this purpose. The assertion density of the code must average a minimum of two
    assertions per function.

  - **[Pair assertions](https://tigerbeetle.com/blog/2023-12-27-it-takes-two-to-contract).** For
    every property you want to enforce, try to find at least two different code paths where an
    assertion can be added. For example, assert validity of data right before writing it to disk,
    and also immediately after reading from disk.

  - On occasion, you may use a blatantly true assertion instead of a comment as stronger
    documentation where the assertion condition is critical and surprising.

  - Split compound assertions: prefer `assert(a); assert(b);` over `assert(a and b);`.
    The former is simpler to read, and provides more precise information if the condition fails.

  - Use single-line `if` to assert an implication: `if (a) assert(b)`.

  - **Assert the relationships of compile-time constants** as a sanity check, and also to document
    and enforce [subtle
    invariants](https://github.com/coilhq/tigerbeetle/blob/db789acfb93584e5cb9f331f9d6092ef90b53ea6/src/vsr/journal.zig#L45-L47)
    or [type
    sizes](https://github.com/coilhq/tigerbeetle/blob/578ac603326e1d3d33532701cb9285d5d2532fe7/src/ewah.zig#L41-L53).
    Compile-time assertions are extremely powerful because they are able to check a program's design
    integrity _before_ the program even executes.

  - **The golden rule of assertions is to assert the _positive space_ that you do expect AND to
    assert the _negative space_ that you do not expect** because where data moves across the
    valid/invalid boundary between these spaces is where interesting bugs are often found. This is
    also why **tests must test exhaustively**, not only with valid data but also with invalid data,
    and as valid data becomes invalid.

  - Assertions are a safety net, not a substitute for human understanding. With simulation testing,
    there is the temptation to trust the fuzzer. But a fuzzer can prove only the presence of bugs,
    not their absence. Therefore:
    - Build a precise mental model of the code first,
    - encode your understanding in the form of assertions,
    - write the code and comments to explain and justify the mental model to your reviewer,
    - and use fuzz testing as the final line of defense, to find bugs in your and reviewer's
      understanding of code.

- All memory must be statically allocated at startup. **No memory may be dynamically allocated (or
  freed and reallocated) after initialization.** This avoids unpredictable behavior that can
  significantly affect performance, and avoids use-after-free. As a second-order effect, it is our
  experience that this also makes for more efficient, simpler designs that are more performant and
  easier to maintain and reason about, compared to designs that do not consider all possible memory
  usage patterns upfront as part of the design.

  _Divergence from TigerBeetle:_ TigerBeetle enforces this absolutely via a `StaticAllocator` that
  crashes on any allocation after startup (see `docs/references/tigerbeetle/src/static_allocator.zig`).
  homestead-smelter has two trust domains with different constraints:

  - **Guest agent** (runs inside untrusted Firecracker VM): Must be zero-allocation after init. The
    guest collects fixed-schema metrics from procfs into a fixed-size frame buffer. There is no reason
    to allocate after startup. Any allocation here is a bug.

  - **Host agent** (runs on trusted bare metal): May use bounded dynamic allocation for VM discovery,
    because the number of VMs is not known at compile time. Use `ArenaAllocator` per logical unit of
    work (discovery cycle, snapshot serialization) so that all temporaries are bulk-freed. Never use
    `GeneralPurposeAllocator` in the host agent's steady state.

- Declare variables at the **smallest possible scope**, and **minimize the number of variables in
  scope**, to reduce the probability that variables are misused.

- There's a sharp discontinuity between a function fitting on a screen, and having to scroll to
  see how long it is. For this physical reason we enforce a **hard limit of 70 lines per function**.
  Art is born of constraints. There are many ways to cut a wall of code into chunks of 70 lines,
  but only a few splits will feel right. Some rules of thumb:

  * Good function shape is often the inverse of an hourglass: a few parameters, a simple return
    type, and a lot of meaty logic between the braces.
  * Centralize control flow. When splitting a large function, try to keep all switch/if
    statements in the "parent" function, and move non-branchy logic fragments to helper
    functions. Divide responsibility. All control flow should be handled by _one_ function, the rest shouldn't
    care about control flow at all. In other words,
    ["push `if`s up and `for`s down"](https://matklad.github.io/2023/11/15/push-ifs-up-and-fors-down.html).
  * Similarly, centralize state manipulation. Let the parent function keep all relevant state in
    local variables, and use helpers to compute what needs to change, rather than applying the
    change directly. Keep leaf functions pure.

- Appreciate, from day one, **all compiler warnings at the compiler's strictest setting**.

- Whenever your program has to interact with external entities, **don't do things directly in
  reaction to external events**. Instead, your program should run at its own pace. Not only does
  this make your program safer by keeping the control flow of your program under your control, it
  also improves performance for the same reason (you get to batch, instead of context switching on
  every event). Additionally, this makes it easier to maintain bounds on work done per time period.

Beyond these rules:

- Compound conditions that evaluate multiple booleans make it difficult for the reader to verify
  that all cases are handled. Split compound conditions into simple conditions using nested
  `if/else` branches. Split complex `else if` chains into `else { if { } }` trees. This makes the
  branches and cases clear. Again, consider whether a single `if` does not also need a matching
  `else` branch, to ensure that the positive and negative spaces are handled or asserted.

- Negations are not easy! State invariants positively. When working with lengths and indexes, this
  form is easy to get right (and understand):

  ```zig
  if (index < length) {
    // The invariant holds.
  } else {
    // The invariant doesn't hold.
  }
  ```

  This form is harder, and also goes against the grain of how `index` would typically be compared to
  `length`, for example, in a loop condition:

  ```zig
  if (index >= length) {
    // It's not true that the invariant holds.
  }
  ```

- All errors must be handled. An [analysis of production failures in distributed data-intensive
  systems](https://www.usenix.org/system/files/conference/osdi14/osdi14-paper-yuan.pdf) found that
  the majority of catastrophic failures could have been prevented by simple testing of error
  handling code.

> “Specifically, we found that almost all (92%) of the catastrophic system failures are the result
> of incorrect handling of non-fatal errors explicitly signaled in software.”

### Wire Format

homestead-smelter communicates over vsock using fixed-size binary frames. The wire format is the most
security-relevant surface in the codebase — a malformed frame from a compromised guest must never
cause undefined behavior on the host.

- **All wire-format integers are little-endian.** Use `std.mem.writeInt(T, buf, value, .little)` and
  `std.mem.readInt(T, buf, .little)` exclusively. Never use `@bitCast` or direct memory
  reinterpretation for wire data.

- **Frame structs must have compile-time size and padding assertions.** For every struct that appears
  on the wire, add:

  ```zig
  comptime {
      std.debug.assert(@sizeOf(SampleFrame) == expected_size);
      // Verify no implicit padding between fields.
      std.debug.assert(@bitSizeOf(SampleFrame) == @sizeOf(SampleFrame) * 8);
  }
  ```

  See TigerBeetle's pattern: `assert(@sizeOf(TransferPending) == 16); assert(stdx.no_padding(TransferPending));`
  in `docs/references/tigerbeetle/src/state_machine.zig`.

- **Pair assertions on encode and decode.** Every field written during `encodeHelloFrame` should have
  a symmetric read in `decodeHelloFrame`. Assert that the decoded values satisfy the same invariants
  as the pre-encode values. This catches serialization bugs at both boundaries rather than one.

- **Validate all fields from untrusted input.** The host must validate every field of a frame
  received from a guest before using it. Do not trust `flags`, `seq`, timestamps, or string fields.
  Return `FrameError` for invalid data — never `unreachable` or `@panic` on guest-supplied bytes.

### Threading

The host agent is multi-threaded: one thread per VM connection, plus a main thread for discovery and
control socket handling. Shared state is protected by `std.Thread.Mutex`.

- **Minimize critical section scope.** Hold the mutex only while reading or writing shared state.
  Never perform I/O, allocation, or blocking operations while holding the lock. Copy what you need
  out of the shared state, release the lock, then operate on the copy.

- **Single mutex, flat hierarchy.** The host agent uses one mutex for `AgentState`. Do not introduce
  a second mutex unless the contention is measured and proven. Multiple mutexes invite deadlock.
  If a second lock becomes necessary, document the lock ordering.

- **Threads must not outlive their data.** When a VM is removed from discovery, the reader thread
  for that VM must be signaled to exit and joined before the VM's state is freed. Use
  `std.Thread.join()` — do not detach threads.

- **No atomics unless the mutex is a measured bottleneck.** `std.Thread.Mutex` is correct and
  auditable. Lock-free code is subtle and difficult to assert against. The host agent's thread count
  (one per VM, typically <100) does not warrant lock-free data structures.

- **Always motivate, always say why**. Never forget to say why. Because if you explain the rationale
  for a decision, it not only increases the hearer's understanding, and makes them more likely to
  adhere or comply, but it also shares criteria with them with which to evaluate the decision and
  its importance.

- **Explicitly pass options to library functions at the call site, instead of relying on the
  defaults**. For example, write `@prefetch(a, .{ .cache = .data, .rw = .read, .locality = 3 });`
  over `@prefetch(a, .{});`. This improves readability but most of all avoids latent, potentially
  catastrophic bugs in case the library ever changes its defaults.

## Performance

> “The lack of back-of-the-envelope performance sketches is the root of all evil.” — Rivacindela
> Hudsoni

- Think about performance from the outset, from the beginning. **The best time to solve performance,
  to get the huge 1000x wins, is in the design phase, which is precisely when we can't measure or
  profile.** It's also typically harder to fix a system after implementation and profiling, and the
  gains are less. So you have to have mechanical sympathy. Like a carpenter, work with the grain.

- **Perform back-of-the-envelope sketches with respect to the four resources (network, disk, memory,
  CPU) and their two main characteristics (bandwidth, latency).** Sketches are cheap. Use sketches
  to be “roughly right” and land within 90% of the global maximum.

- Optimize for the slowest resources first (network, disk, memory, CPU) in that order, after
  compensating for the frequency of usage, because faster resources may be used many times more. For
  example, a memory cache miss may be as expensive as a disk fsync, if it happens many times more.

- Distinguish between the control plane and data plane. A clear delineation between control plane
  and data plane through the use of batching enables a high level of assertion safety without losing
  performance. See [TigerBeetle's talk on Zig SHOWTIME](https://youtu.be/BH2jvJ74npM?t=1958) for
  examples.

- Amortize network, disk, memory and CPU costs by batching accesses.

- Let the CPU be a sprinter doing the 100m. Be predictable. Don't force the CPU to zig zag and
  change lanes. Give the CPU large enough chunks of work. This comes back to batching.

- Be explicit. Minimize dependence on the compiler to do the right thing for you.

  In particular, extract hot loops into stand-alone functions with primitive arguments without
  `self` (see [an example](https://github.com/tigerbeetle/tigerbeetle/blob/0.16.19/src/lsm/compaction.zig#L1932-L1937)).
  That way, the compiler doesn't need to prove that it can cache struct's fields in registers, and a
  human reader can spot redundant computations easier.

## Developer Experience

> “There are only two hard things in Computer Science: cache invalidation, naming things, and
> off-by-one errors.” — Phil Karlton

### Naming Things

- **Get the nouns and verbs just right.** Great names are the essence of great code, they capture
  what a thing is or does, and provide a crisp, intuitive mental model. They show that you
  understand the domain. Take time to find the perfect name, to find nouns and verbs that work
  together, so that the whole is greater than the sum of its parts.

- Use `snake_case` for function, variable, and file names. The underscore is the closest thing we
  have as programmers to a space, and helps to separate words and encourage descriptive names. We
  don't use Zig's `CamelCase.zig` style for "struct" files to keep the convention simple and
  consistent.

- Do not abbreviate variable names, unless the variable is a primitive integer type used as an
  argument to a sort function or matrix calculation. Use long form arguments in scripts: `--force`,
  not `-f`. Single letter flags are for interactive usage.

- Use proper capitalization for acronyms (`VSRState`, not `VsrState`).

- For the rest, follow the Zig style guide.

- Add units or qualifiers to variable names, and put the units or qualifiers last, sorted by
  descending significance, so that the variable starts with the most significant word, and ends with
  the least significant word. For example, `latency_ms_max` rather than `max_latency_ms`. This will
  then line up nicely when `latency_ms_min` is added, as well as group all variables that relate to
  latency.

- Infuse names with meaning. For example, `allocator: Allocator` is a good, if boring name,
  but `gpa: Allocator` and `arena: Allocator` are excellent. They inform the reader whether
  `deinit` should be called explicitly.

- When choosing related names, try hard to find names with the same number of characters so that
  related variables all line up in the source. For example, as arguments to a memcpy function,
  `source` and `target` are better than `src` and `dest` because they have the second-order effect
  that any related variables such as `source_offset` and `target_offset` will all line up in
  calculations and slices. This makes the code symmetrical, with clean blocks that are easier for
  the eye to parse and for the reader to check.

- When a single function calls out to a helper function or callback, prefix the name of the helper
  function with the name of the calling function to show the call history. For example,
  `read_sector()` and `read_sector_callback()`.

- Callbacks go last in the list of parameters. This mirrors control flow: callbacks are also
  _invoked_ last.

- _Order_ matters for readability (even if it doesn't affect semantics). On the first read, a file
  is read top-down, so put important things near the top. The `main` function goes first.

  The same goes for `structs`, the order is fields then types then methods:

  ```zig
  time: Time,
  process_id: ProcessID,

  const ProcessID = struct { cluster: u128, replica: u8 };
  const Tracer = @This(); // This alias concludes the types section.

  pub fn init(gpa: std.mem.Allocator, time: Time) !Tracer {
      ...
  }
  ```

  If a nested type is complex, make it a top-level struct.

  At the same time, not everything has a single right order. When in doubt, consider sorting
  alphabetically, taking advantage of big-endian naming.

- Don't overload names with multiple meanings that are context-dependent. For example, homestead-smelter
  uses _connection_ in two contexts: a vsock connection to a Firecracker guest, and a Unix domain
  socket connection from a CLI client. Calling both "connection" causes confusion. Instead, use
  _stream_ for the vsock data path and _control connection_ for the CLI path.

- Think of how names will be used outside the code, in documentation or communication. For example,
  a noun is often a better descriptor than an adjective or present participle, because a noun can be
  directly used in correspondence without having to be rephrased. Compare `guest.heartbeat` vs
  `guest.beating`. The former can be used directly as a section header in a document or
  conversation, whereas the latter must be clarified. Noun names compose more clearly for derived
  identifiers, e.g. `config.heartbeat_interval_ms`.

- Zig has named arguments through the `options: struct` pattern. Use it when arguments can be
  mixed up. A function taking two `u64` must use an options struct. If an argument can be `null`,
  it should be named so that the meaning of `null` literal at the call site is clear.

  Because dependencies like an allocator or a tracer are singletons with unique types, they should
  be threaded through constructors positionally, from the most general to the most specific.

- **Write descriptive commit messages** that inform and delight the reader, because your commit
  messages are being read. Note that a pull request description is not stored in the git repository
  and is invisible in `git blame`, and therefore is not a replacement for a commit message.

- Don't forget to say why. Code alone is not documentation. Use comments to explain why you wrote
  the code the way you did. Show your workings.

- Don't forget to say how. For example, when writing a test, think of writing a description at the
  top to explain the goal and methodology of the test, to help your reader get up to speed, or to
  skip over sections, without forcing them to dive in.

- Comments are sentences, with a space after the slash, with a capital letter and a full stop, or a
  colon if they relate to something that follows. Comments are well-written prose describing the
  code, not just scribblings in the margin. Comments after the end of a line _can_ be phrases, with
  no punctuation.

### Cache Invalidation

- Don't duplicate variables or take aliases to them. This will reduce the probability that state
  gets out of sync.

- If you don't mean a function argument to be copied when passed by value, and if the argument type
  is more than 16 bytes, then pass the argument as `*const`. This will catch bugs where the caller
  makes an accidental copy on the stack before calling the function.

- Construct larger structs _in-place_ by passing an _out pointer_ during initialization.

  In-place initializations can assume **pointer stability** and **immovable types** while
  eliminating intermediate copy-move allocations, which can lead to undesirable stack growth.

  Keep in mind that in-place initializations are viral — if any field is initialized
  in-place, the entire container struct should be initialized in-place as well.

  **Prefer:**
  ```zig
  fn init(target: *LargeStruct) !void {
    target.* = .{
      // in-place initialization.
    };
  }

  fn main() !void {
    var target: LargeStruct = undefined;
    try target.init();
  }
  ```

  **Over:**
  ```zig
  fn init() !LargeStruct {
    return LargeStruct {
      // moving the initialized object.
    }
  }

  fn main() !void {
    var target = try LargeStruct.init();
  }
  ```

- **Shrink the scope** to minimize the number of variables at play and reduce the probability that
  the wrong variable is used.

- Calculate or check variables close to where/when they are used. **Don't introduce variables before
  they are needed.** Don't leave them around where they are not. This will reduce the probability of
  a POCPOU (place-of-check to place-of-use), a distant cousin to the infamous
  [TOCTOU](https://en.wikipedia.org/wiki/Time-of-check_to_time-of-use). Most bugs come down to a
  semantic gap, caused by a gap in time or space, because it's harder to check code that's not
  contained along those dimensions.

- Use simpler function signatures and return types to reduce dimensionality at the call site, the
  number of branches that need to be handled at the call site, because this dimensionality can also
  be viral, propagating through the call chain. For example, as a return type, `void` trumps `bool`,
  `bool` trumps `u64`, `u64` trumps `?u64`, and `?u64` trumps `!u64`.

- Ensure that functions run to completion without suspending, so that precondition assertions are
  true throughout the lifetime of the function. These assertions are useful documentation without a
  suspend, but may be misleading otherwise.

- Be on your guard for **[buffer bleeds](https://en.wikipedia.org/wiki/Heartbleed)**. This is a
  buffer underflow, the opposite of a buffer overflow, where a buffer is not fully utilized, with
  padding not zeroed correctly. This may not only leak sensitive information, but may cause
  deterministic guarantees to be violated.

- Use newlines to **group resource allocation and deallocation**, i.e. before the resource
  allocation and after the corresponding `defer` statement, to make leaks easier to spot.

- Be explicit about **resource ownership transfer**. If a raw file descriptor is wrapped in a
  higher-level owner such as `std.net.Stream`, the raw-fd `defer`/`errdefer` must end at the
  transfer boundary. Do not keep both cleanup paths alive across the same fallible code, or a
  handshake failure will turn into a deterministic double-close and trip Zig's safety checks.

  ```zig
  const stream = blk: {
      const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM, 0);
      errdefer std.posix.close(fd); // valid only before ownership transfer
      try std.posix.connect(fd, &address.any, address.getOsSockLen());
      break :blk std.net.Stream{ .handle = fd };
  };
  errdefer stream.close(); // sole owner after wrapping
  ```

- When a `ReleaseSafe` crash lands in Zig stdlib `unreachable`, reproduce with `-Doptimize=Debug`
  before committing to a root-cause theory. Optimized builds often collapse the local frame that
  actually violated ownership or lifetime rules.

### Off-By-One Errors

- **The usual suspects for off-by-one errors are casual interactions between an `index`, a `count`
  or a `size`.** These are all primitive integer types, but should be seen as distinct types, with
  clear rules to cast between them. To go from an `index` to a `count` you need to add one, since
  indexes are _0-based_ but counts are _1-based_. To go from a `count` to a `size` you need to
  multiply by the unit. Again, this is why including units and qualifiers in variable names is
  important.

- Show your intent with respect to division. For example, use `@divExact()`, `@divFloor()` or
  `div_ceil()` to show the reader you've thought through all the interesting scenarios where
  rounding may be involved.

### Style By The Numbers

- Run `zig fmt`.

- Use 4 spaces of indentation, rather than 2 spaces, as that is more obvious to the eye at a
  distance.

- Hard limit all line lengths, without exception, to at most 100 columns for a good typographic
  "measure". Use it up. Never go beyond. Nothing should be hidden by a horizontal scrollbar. Let
  your editor help you by setting a column ruler. To wrap a function signature, call or data
  structure, add a trailing comma, close your eyes and let `zig fmt` do the rest.

  Similar to function length, the motivation behind the number 100 is physical: just enough
  to fit two copies of the code side-by-side on a screen.

- Add braces to the `if` statement unless it fits on a single line for consistency and defense in
  depth against "goto fail;" bugs.

### Dependencies

We maintain **a “zero Zig dependencies” policy** — no third-party Zig packages beyond the standard
library and toolchain. Dependencies, in
general, inevitably lead to supply chain attacks, safety and performance risk, and slow install
times. For foundational infrastructure in particular, the cost of any dependency is further
amplified throughout the rest of the stack.

### Tooling

Similarly, tools have costs. A small standardized toolbox is simpler to operate than an array of
specialized instruments each with a dedicated manual. Our primary tool is Zig. It may not be the
best for everything, but it's good enough for most things. We invest into our Zig tooling to ensure
that we can tackle new problems quickly, with a minimum of accidental complexity in our local
development environment.

> “The right tool for the job is often the tool you are already using—adding new tools has a higher
> cost than many people appreciate” — John Carmack

For example, the next time you write a script, instead of `scripts/*.sh`, write `scripts/*.zig`.

This not only makes your script cross-platform and portable, but introduces type safety and
increases the probability that running your script will succeed for everyone on the team, instead of
hitting a Bash/Shell/OS-specific issue.

Standardizing on Zig for tooling is important to ensure that we reduce dimensionality, as the team,
and therefore the range of personal tastes, grows. This may be slower for you in the short term, but
makes for more velocity for the team in the long term.

## Code Review: DON'T / DO Examples

Each example below is a compilable, testable Zig program in `docs/zig-coding/examples/`. Run them
with `zig test docs/zig-coding/examples/<file>.zig` from the `homestead-smelter/` directory.

### 1. Allocator Discipline

**File:** [`docs/examples/01_allocator_discipline.zig`](examples/01_allocator_discipline.zig)

**DON'T:** Use `GeneralPurposeAllocator` in a long-lived production agent. GPA is a debug
allocator — it tracks every allocation, detects double-free and use-after-free, and has significant
overhead. It is invaluable during development, but it is not a production allocator.

```zig
// GPA as the production allocator — overhead on every alloc/free,
// fragmentation over time, wrong tool for steady-state operation.
pub fn main() !void {
    var gpa_state = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa_state.deinit();
    const gpa = gpa_state.allocator();

    while (true) {
        const line = try readLineAlloc(gpa, stream, 4096);
        defer gpa.free(line); // individual free per allocation
    }
}
```

**DO:** Allocate a fixed buffer at startup, then use `ArenaAllocator` per connection. The arena
bulk-frees on connection close — no individual `free()` calls, no fragmentation, no use-after-free.

```zig
const Connection = struct {
    arena: std.heap.ArenaAllocator,

    fn init() Connection {
        return .{
            .arena = std.heap.ArenaAllocator.init(std.heap.page_allocator),
        };
    }

    fn deinit(self: *Connection) void {
        self.arena.deinit(); // bulk-free everything
    }

    fn handleRequest(self: *Connection, request: []const u8) ![]const u8 {
        const allocator = self.arena.allocator();
        // Allocations are "fire and forget" — the arena owns them.
        return try std.fmt.allocPrint(allocator, "ACK {d} bytes: {s}", .{ request.len, request });
    }
};
```

For even tighter control, use `FixedBufferAllocator` with a stack buffer when the maximum size is
known at comptime — zero syscalls, zero heap interaction.

### 2. Buffered I/O

**File:** [`docs/examples/02_buffered_io.zig`](examples/02_buffered_io.zig)

**DON'T:** Read one byte at a time with a syscall per byte. Each `read(2)` call crosses the
user/kernel boundary. For a heartbeat agent called continuously, this means N syscalls per N-byte
line.

```zig
pub fn readLineAlloc(allocator: Allocator, stream: Stream, limit: usize) ![]u8 {
    var bytes = try std.ArrayList(u8).initCapacity(allocator, 64);
    var one: [1]u8 = undefined;
    while (true) {
        const n = try stream.read(one[0..]);  // one syscall per byte!
        if (n == 0) break;
        if (one[0] == '\n') break;
        if (bytes.items.len >= limit) return error.LineTooLong;
        try bytes.append(allocator, one[0]);
    }
    return bytes.toOwnedSlice(allocator);
}
```

**DO:** Read into a caller-provided buffer. Use `readByte()` on a buffered reader so the underlying
reads happen in chunks. No allocator needed — the caller controls the memory.

```zig
pub fn readLine(reader: anytype, output: []u8) !?[]u8 {
    var pos: usize = 0;
    while (pos < output.len) {
        const byte = reader.readByte() catch |err| switch (err) {
            error.EndOfStream => {
                if (pos == 0) return null;
                return output[0..pos];
            },
            else => return err,
        };
        switch (byte) {
            '\n' => return output[0..pos],
            '\r' => continue,
            else => {
                output[pos] = byte;
                pos += 1;
            },
        }
    }
    return error.LineTooLong;
}
```

### 3. JSON Serialization

**File:** [`docs/examples/03_json_serialization.zig`](examples/03_json_serialization.zig)

**DON'T:** Hand-roll JSON serialization with manual string escaping. It is verbose, error-prone
(easy to miss an escape character or control byte), and every new field requires modifying the
serialization function.

```zig
fn buildSnapshotJSON(allocator: Allocator, ...) ![]u8 {
    var out: std.ArrayList(u8) = .{};
    defer out.deinit(allocator);
    try out.ensureTotalCapacity(allocator, 512);
    try out.appendSlice(allocator, "{\"schema_version\":1,\"jailer_root\":");
    try appendJSONString(&out, allocator, jailer_root);
    try out.appendSlice(allocator, ",\"guest_port\":");
    try appendInt(&out, allocator, guest_port);
    // ... 40 more lines of manual field-by-field serialization ...
    return out.toOwnedSlice(allocator);
}
```

**DO:** Use typed structs and `std.json.Stringify.valueAlloc`. The compiler generates serialization
code at comptime for any struct. Adding a field to the struct automatically includes it in the
output — no serialization code to update, no escape bugs.

```zig
const Snapshot = struct {
    schema_version: u32 = 1,
    jailer_root: []const u8,
    guest_port: u32,
    observed_at_unix_ms: i64,
    vms: []const VMObservation,
};

fn serializeSnapshot(allocator: std.mem.Allocator, snapshot: Snapshot) ![]u8 {
    return std.json.Stringify.valueAlloc(allocator, snapshot, .{});
}
```

### 4. Wire Protocol as Tagged Union

**File:** [`docs/examples/04_protocol_types.zig`](examples/04_protocol_types.zig)

**DON'T:** Dispatch on raw strings with chained `if`/`else`. Adding a new command requires updating
the dispatch, the argument parser, and the handler — with no compiler assistance if you forget one.

```zig
fn handleControlConnection(stream: Stream) !void {
    const line = try readLine(stream);
    if (std.mem.eql(u8, line, "PING")) {
        try writeLine(stream, "PONG homestead-smelter-host");
        return;
    }
    if (std.mem.eql(u8, line, "SNAPSHOT")) {
        try writeSnapshotResponse(stream);
        return;
    }
    // Forgot to handle "STATUS"? No compiler error. Silent bug.
    try writeLine(stream, "ERR unsupported command");
}
```

**DO:** Model the wire protocol as a tagged union. The compiler enforces exhaustive switches — if
you add a variant, every `switch` must handle it or the build fails.

```zig
const Command = union(enum) {
    ping,
    snapshot,
    probe: ProbeParams,

    const ProbeParams = struct { job_id: []const u8 };

    fn parse(line: []const u8) ?Command {
        if (std.mem.eql(u8, line, "PING")) return .ping;
        if (std.mem.eql(u8, line, "SNAPSHOT")) return .snapshot;
        if (std.mem.startsWith(u8, line, "PROBE ")) {
            const job_id = line["PROBE ".len..];
            if (job_id.len == 0) return null;
            return .{ .probe = .{ .job_id = job_id } };
        }
        return null;
    }
};

// Exhaustive switch — add a variant, get a compile error here.
fn dispatch(command: Command) Response {
    return switch (command) {
        .ping => .pong,
        .snapshot => .{ .snapshot = "{\"schema_version\":1}" },
        .probe => |params| .{ .probe_result = .{ .job_id = params.job_id, ... } },
    };
}
```

### 5. Assertion Density

**File:** [`docs/examples/05_assertions.zig`](examples/05_assertions.zig)

**DON'T:** Write functions with no assertions, relying on callers to provide correct inputs. Bugs
silently propagate until they corrupt data or crash far from the root cause.

```zig
fn readLineAlloc(allocator: Allocator, stream: Stream, limit: usize) ![]u8 {
    // No assertion that limit > 0 — a limit of 0 silently returns empty.
    // No assertion on the result — could return garbage on partial read.
    var one: [1]u8 = undefined;
    while (true) {
        const n = try stream.read(one[0..]);
        if (n == 0) break;
        if (one[0] == '\n') break;
        if (bytes.items.len >= limit) return error.LineTooLong;
        try bytes.append(allocator, one[0]);
    }
    return bytes.toOwnedSlice(allocator);
}
```

**DO:** Assert preconditions, postconditions, and invariants. Minimum two assertions per function.
Assert both what you expect (positive space) and what you do not expect (negative space). Use
`comptime` assertions for design integrity.

```zig
// Compile-time assertions — zero runtime cost, caught before the program executes.
comptime {
    std.debug.assert(max_snapshot_bytes >= max_line_bytes);
    std.debug.assert(default_guest_port >= 1024);
}

fn readLine(reader: anytype, buf: []u8) !?[]u8 {
    // Precondition: buffer must be usable.
    std.debug.assert(buf.len > 0);

    var pos: usize = 0;
    while (pos < buf.len) {
        const byte = reader.readByte() catch |err| switch (err) {
            error.EndOfStream => {
                if (pos == 0) return null;
                // Postcondition: no control characters in output.
                assertNoControlChars(buf[0..pos]);
                return buf[0..pos];
            },
            else => return err,
        };
        switch (byte) {
            '\n' => {
                assertNoControlChars(buf[0..pos]);
                return buf[0..pos];
            },
            '\r' => continue,
            else => { buf[pos] = byte; pos += 1; },
        }
    }
    return error.LineTooLong;
}
```

See the example file for a `RingBuffer` that demonstrates pre/postcondition assertions on every
push and pop, plus compile-time power-of-two validation.

### 6. @ptrCast Encapsulation

**File:** [`docs/examples/06_ptrcast_encapsulation.zig`](examples/06_ptrcast_encapsulation.zig)

**DON'T:** Scatter `@ptrCast` at every call site. Each cast is a potential safety hole. Repeating
the pattern increases the probability of getting it wrong or forgetting `@alignCast`.

```zig
fn bindVsock(fd: std.posix.fd_t, port: u32) !void {
    var address = linux.sockaddr.vm{ .port = port, .cid = std.math.maxInt(u32), .flags = 0 };
    // @ptrCast repeated at every call site
    try std.posix.bind(fd, @ptrCast(&address), @sizeOf(linux.sockaddr.vm));
}

fn connectVsock(fd: std.posix.fd_t, cid: u32, port: u32) !void {
    var address = linux.sockaddr.vm{ .port = port, .cid = cid, .flags = 0 };
    // Same @ptrCast duplicated — violates DRY
    try std.posix.connect(fd, @ptrCast(&address), @sizeOf(linux.sockaddr.vm));
}
```

**DO:** Encapsulate the cast in a typed helper. The `@ptrCast` appears exactly once. Call sites are
clean and type-safe.

```zig
const VsockAddress = struct {
    cid: u32,
    port: u32,

    const cid_any: u32 = std.math.maxInt(u32);
    const cid_host: u32 = 2;

    fn toSockaddr(self: VsockAddress) struct { addr: linux.sockaddr.vm, len: u32 } {
        return .{
            .addr = .{ .port = self.port, .cid = self.cid, .flags = 0 },
            .len = @sizeOf(linux.sockaddr.vm),
        };
    }

    fn bind(self: VsockAddress, fd: std.posix.fd_t) !void {
        var sa = self.toSockaddr();
        try std.posix.bind(fd, @ptrCast(&sa.addr), sa.len); // cast appears once
    }

    fn connect(self: VsockAddress, fd: std.posix.fd_t) !void {
        var sa = self.toSockaddr();
        try std.posix.connect(fd, @ptrCast(&sa.addr), sa.len); // cast appears once
    }
};
```

### 7. Comptime Configuration

**File:** [`docs/examples/07_comptime_config.zig`](examples/07_comptime_config.zig)

**DON'T:** Use runtime constants for values known at build time. The compiler cannot optimize buffer
sizes, eliminate branches, or validate constraints.

```zig
pub const default_guest_port: u32 = 10790;
pub const max_line_bytes: usize = 4096;
// These are "constants" but the compiler treats them as runtime values
// in some contexts. No compile-time validation possible.
```

**DO:** Use comptime constants with compile-time validation. Invalid configs never produce a binary.
Use `build.zig` options for values that vary per build target.

```zig
const Config = struct {
    guest_port: u32,
    max_line_bytes: usize,
    max_snapshot_bytes: usize,

    fn validate(comptime self: Config) Config {
        if (self.guest_port < 1024)
            @compileError("guest_port must be >= 1024 (non-privileged)");
        if (self.max_snapshot_bytes < self.max_line_bytes)
            @compileError("max_snapshot_bytes must be >= max_line_bytes");
        return self;
    }
};

const config = (Config{
    .guest_port = 10790,
    .max_line_bytes = 4096,
    .max_snapshot_bytes = 1024 * 1024,
}).validate(); // validated at compile time — zero runtime cost

// Comptime-sized types — the compiler knows the exact buffer size.
fn LineReader(comptime max_bytes: usize) type {
    return struct {
        buf: [max_bytes]u8 = undefined,
        len: usize = 0,
        // ...
    };
}
```
