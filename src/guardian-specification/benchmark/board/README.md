# Board Benchmark

Guardian owns a pinned `hyperfine` binary for command-level board benchmarks.

```sh
bazelisk run //src/guardian-specification/benchmark/board:hyperfine -- \
  --warmup 1 \
  --runs 5 \
  './bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian board src/guardian-specification/examples/gamma/gamma.cue --repo-root "$PWD" -o json'
```

The runnable tool bytes are declared in
`src/guardian-specification/tools.lock.json`. The matching upstream source is
declared as a commit-addressed archive in
`src/guardian-specification/guardian_tools.MODULE.bazel`.
