# Preflight Benchmark

Guardian owns a pinned `hyperfine` binary for command-level preflight benchmarks.

```sh
guardian run bazel -- build //src/guardian-specification/benchmark/preflight:hyperfine
build_output="$(guardian run bazel -- cquery --output=files //src/guardian-specification/benchmark/preflight:hyperfine)"
"$build_output" \
  --warmup 1 \
  --runs 5 \
  'guardian preflight -f src/guardian-specification/examples/gamma/gamma.cue -o json'
```

Run this from the repo root with `guardian` on `PATH`. The benchmarked command
is the normal CLI invocation, not a site-specific command.

The runnable tool bytes are declared in
`src/guardian-specification/tools.lock.json`. The matching upstream source is
declared as a commit-addressed archive in
`src/guardian-specification/guardian_tools.MODULE.bazel`.
