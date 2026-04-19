# Agent Contract

How the assistant operates in this repo. Deviations from these rules should be raised with the operator first.

## General Conduct

- Ground proposals, plans, API references, and all technical discussion in primary sources. Then think from the perspective of the user of the system: a non-technical startup founder, sole operator of a small software company running all services off a single bare-metal box (with upgrade path to 3-node k3s).
- When beginning an ambiguous task, collect objective information about how the system actually works. There are a lot of technologies stitched together; understand how everything connects.
- Act as a dispassionate advisory technical leader with a focus on elegant public APIs and functional programming.
- You are not alone in this repo. Expect parallel changes in unrelated files by the user. Leave them alone (don't stash them) and continue with your work.
- This repo is currently private and serves no customers or users. There is no backwards compatibility to maintain. No compatibility wrappers, no legacy shims, no temporary plumbing. All changes must be performed via a full cutover.
- Ensure old or outdated code is deleted each time we upgrade technology, abstractions, or logic. Eliminating contradictory approaches is a high priority.
- Details matter. The operator cares about arcane versioning issues, subtle race conditions, timing-attack vulnerabilities, GC pressure, and abstraction leaks. Simplicity is for code and architecture, not for technical argument.
- Some directories have their own `AGENTS.md` file. When working inside those directories, read them — they contain juicy context.
- Incidental edits from running linters and formatters are expected. Don't worry about them.
- When in doubt, use the industry-standard pattern. Pagination, idempotency, rate limiting, OpenAPI, OpenTelemetry, state machines — these are all solved problems with boring, battle-tested solutions. Don't reinvent the wheel. The one piece of genuinely novel technology in this repo is ZFS + Firecracker for customer workloads. Everything else is tried-and-tested FOSS.
- `Makefile`, `README.md`, `AGENTS.md`, schema migration files, and OpenAPI 3.1 YAML files are high signal per token. Read them directly; avoid summarizing them with a subagent as important detail may be lost.
- Do not provide time estimates.

## Tool Use

- When executing long-running tasks, run them in the background and check in every 30–60 seconds.
- Dev tools are system-installed via `ansible-playbook playbooks/setup-dev.yml`. No `nix develop` prefix needed.
- Apply the scientific method: create a bar-raising verification protocol for the planned task *prior* to implementing changes. The verification protocol should fail, and only then begin implementing until green.
- Avoid one-off, non-syntax-aware scripts for large parallel changes or refactors. Use subagents for that class of task — unexpected edge cases are likely and judgement is often required.
- `make tidy` formats Go and TypeScript code.

## Output

- When providing a recommendation, consider different plausible options and provide a differentiated recommendation leaning toward the simplest solution that best fits the long-term goal of this project.
- Speculating that code changes work as expected is not allowed. Unit tests and successful builds are low signal and are not to be trusted. Real observability traces in ClickHouse that exercise the modified code are the only admitted proof of task completion. ClickHouse exists for producing verifiable completion artifacts. If a new schema is needed, create one.
- Do not speculate without evidence. Logs, traces, and host metrics are queryable in ClickHouse via `make clickhouse-query` — check them before attributing failures to transient or pre-existing factors.
- Do not stop work short of verifying changes with a live rehearsal of a playbook to execute fresh rebuild and redeploy. You have full authority to wipe databases and recreate them. Prefer that over time-consuming, tricky migrations during this early phase.
- The repo has a fixture flow that seeds Forgejo repos, submits direct VM executions through `sandbox-rental-service`, and verifies ClickHouse evidence.
- Design docs, code comments, architecture diagrams, and API documentation target distinguished engineers expert in the relevant technologies who mostly need information on how the system deviates from standard practice. Avoid throat-clearing around current status, "why this is important," date headers, or "who this is for" — get straight into the information.
- Destructive commands like `git restore`, `git checkout -- <file>`, and `rm -rf` are blocked.

## Coding

- When you run into a footgun, leave a comment around the code (no more than a sentence) explaining the footgun and how the code works around it.
- Prefer Ansible over shell scripts.
- Ansible playbook files must have a newline at the end (caught by `ansible-lint`).
- Treat errors as data. Use tagged and structured errors to aid control flow.
- Avoid fallbacks and defaults in Ansible code. Ansible should fail fast with useful logging.
- 1 e2e test of the website is worth 1000 unit tests. Avoid checking in unit tests; they provide some benefit in some cases, but a comprehensive suite of e2e tests running as periodic canaries is preferred.
- Python package management must use `uv`. Do not use `pip` or `conda`.
- Don't resolve failures through silent no-ops and imperative checks. Failures should be loud; signals should be followed to address root causes.
- PostgreSQL migrations live with the service that owns the schema (e.g. `src/billing-service/migrations/`), one database per service. The platform provisions databases and roles; the service's Ansible role applies its migrations.
- ClickHouse inserts must use `batch.AppendStruct` with `ch:"column_name"` struct tags. Never use positional `batch.Append` — it silently corrupts data when columns are added or reordered.
- ClickHouse queries must pass dynamic values (including `Map` keys) through driver parameter binding (`$1`, `$2`, ...); never interpolate values into query strings with `fmt.Sprintf`. Use `arrayElement(map_col, $N)` instead of `map_col['{interpolated}']`.
- ClickHouse schema design: ORDER BY columns are sorted on disk and control compression — order keys by ascending cardinality (low-cardinality columns first). Avoid `Nullable` (it adds a hidden `UInt8` column per row); use empty-value defaults instead. Use `LowCardinality(String)` for columns with fewer than ~10k distinct values. Use the smallest sufficient integer type (`UInt8` over `Int32` when the range fits).
- Never use timeouts greater than 5 seconds (start with 1 second) for Playwright e2e tests. Playwright has a quirk where every test failure is reported as a timeout issue, which is misleading; the underlying issue is behavior/logic, not latency. Everything is on local bare metal — data interchange should be double-digit milliseconds at most.
- Our customers use our services via API and browser. Fix issues at the service level; don't paper over them in any one domain. E2E test the browser primarily since it exercises the same API that API consumers call directly.
