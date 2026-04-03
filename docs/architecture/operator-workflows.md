# Operator Workflows

## Deployment Surface

Use `make deploy` for normal platform iteration. It rebuilds the Nix server profile, pushes it to the current worker, and reapplies the Ansible roles without wiping host state.

Use `make deploy-dashboards` when the change is limited to HyperDX sources or dashboards. That path exists specifically so dashboard iteration does not require a full platform redeploy.

## CI Fixture Surface

Use `make ci-fixtures-pass` for the common operator loop: seed the controlled example repositories, warm their goldens if needed, open PRs, and verify that the positive fixture suite succeeds on the already-deployed host.

Use `make ci-fixtures-refresh` when the guest kernel, rootfs, or staged CI artifacts changed. It rebuilds and restages the Firecracker guest artifacts without touching the rest of the platform.

Use `make ci-fixtures-full` when you want the composed rehearsal: refresh guest artifacts first, then run the configured fixture suite set. The orchestration is suite-based so additional suites such as `fail` can be added without changing the operator entrypoints.

## Suite Model

The current suite is `pass`. It contains the positive example repositories that are expected to complete with a successful Forgejo Actions result.

Future suites should describe intent, not implementation details. `fail` should encode repositories that are expected to fail deterministically. `full` is not a suite itself; it is the orchestration target that runs the configured suite list after refresh.
