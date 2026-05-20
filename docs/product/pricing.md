# Pricing and capacity model

This model informs two decisions: whether to provision additional bare-metal capacity for a billing period (capacity planning), and how to run the capacity that exists (capacity management). It is parameterized by pricing mode and by a per-resource price vector so that both decisions are computed under whichever billing mode and rate card are active.

## Billing model

Metering is per-resource at millisecond resolution. A sandbox consumes compute, memory, execution-root storage, durable storage, and north/south traffic independently; each is metered against its own SKU rather than as a bundled instance-hour. The canonical product mode is metered per-resource. A bundled mode (a fixed RAM+vCPU+storage+traffic shape billed per unit time) is a selectable alternative; `products.billing_model` and `plans.billing_mode` carry the switch (`metered` versus `prepaid`/bundled), and the capacity model takes pricing mode as an input rather than assuming one.

The metered pipeline is:

```
consumed resource-ms × plan_sku_rates.unit_rate  = component charge-units
Σ component charge-units                          = billed charge-units
billed charge-units ÷ 10,000,000                  = USD
```

`unit_rate` is charge-units per resource-millisecond, stored per `(plan_id, sku_id)` in `plan_sku_rates`. The charge-unit to currency scalar is `ledgerUnitsPerCent = 100_000` (`src/services/billing-service/internal/billing/ids.go`), so one US dollar is 10,000,000 charge-units. The `sandbox-default` plan defines three SKUs (`src/services/billing-service/cmd/billing-seed/main.go`): `sandbox_compute_amd_epyc_4484px_vcpu_ms` (vCPU-ms), `sandbox_memory_standard_gib_ms` (GiB-ms), and `sandbox_execution_root_storage_premium_nvme_gib_ms` (GiB-ms). Durable storage and north/south traffic are additional resources that require their own SKUs.

The identity converting a `unit_rate` to a human price is:

```
USD per resource-hour = unit_rate × 1000 ms/s × 3600 s/h ÷ 10,000,000
                      = unit_rate × 0.36
```

## Rate-card calibration state

The seeded rates are pre-release placeholders and do not represent target prices:

| SKU | seed `unit_rate` | implied price |
|---|---|---|
| compute (vCPU-ms) | 325 | $117.00 / vCPU-hour |
| memory (GiB-ms) | 40 | $14.40 / GiB-hour |
| execution-root storage (GiB-ms) | 10 | $3.60 / GiB-hour |

The dogfood organization holds a granted prepaid balance of 10^11 charge-units (≈$10,000), which masks the placeholder rates during dogfooding. Capacity economics are computed against a target price vector, and the rate card is then calibrated to that vector by inverting the identity above (`unit_rate = USD-per-resource-hour ÷ 0.36`).

Target price vector (compute anchored to market at roughly half GitHub Actions and at or under Blacksmith):

| Resource | Target price | Equivalent `unit_rate` |
|---|---|---|
| compute | $0.12 / vCPU-hour ($0.002 / vCPU-minute) | 0.333 |
| memory | $0.005 / GiB-hour | 0.0139 |
| execution-root storage | ~$0.001 / GiB-hour | ~0.003 |
| durable storage | $0.10 / GiB-month | per-month SKU |
| north/south egress | $0.01 / GB | per-GB SKU |

GitHub Actions Linux 2-vCPU runners bill at $0.008/minute ($0.24/vCPU-hour); the target compute price is half that at roughly double throughput, consistent with the Blacksmith comparison the product benchmarks against.

## Host cost inputs

Production capacity planning reads the Latitude plan catalog from
`verself.latitude_plan_prices`, keyed by plan and site. The planning default for
paid hosted CI workers is Latitude `f4.metal.medium`: AMD 4564P, 16 physical
cores / 32 hardware threads at 4.5 GHz, 128 GiB RAM, 2 x 480 GB NVMe plus
2 x 1.9 TB NVMe, and 2 x 10 Gbps networking. The sellable durable-storage
number is a site policy derived from the ZFS pool layout, OS reservation, pool
slack, and replication policy; calculations below use compute as the binding
resource.

Representative United States catalog rows:

| Role | Plan | CPU | RAM | Local NVMe | US catalog price | Use |
|---|---|---:|---:|---|---:|---|
| Entry premium / dogfood | `f4.metal.small` | 12c @ 4.4 GHz | 96 GiB | 2 x 960 GB | $398/mo | Low-concurrency high-frequency workers and early dogfood nodes. |
| Default premium | `f4.metal.medium` | 16c @ 4.5 GHz | 128 GiB | 2 x 480 GB + 2 x 1.9 TB | $555/mo | Default paid CI worker class. |
| Flagship premium | `f4.metal.large` | 24c @ 4.1 GHz | 768 GiB | 2 x 480 GB + 2 x 3.8 TB | $1,916/mo | Large monorepos, JVM/browser-heavy suites, and customers buying lowest wall-clock time. |
| Economy / backfill | `m4.metal.medium` | 16c @ 3.0 GHz | 128 GiB | 2 x 480 GB + 2 x 1.9 TB | $456/mo | Backfill capacity after premium queues are clear. |
| Economy / migration pool | `c3.large.x86` / `s3.large.x86` | 24c @ 2.85 GHz | 256-512 GiB | 2 x 1.9-3.8 TB | $496-$650/mo | Cost-optimized, lower-clock workloads and migration capacity. |
| Storage/cache dense | `rs4.metal.large` / `rs4.metal.xlarge` | 32-64c @ 3.1-3.25 GHz | 768-1536 GiB | 2 x 480 GB + 2-4 x 8 TB | $2,351-$3,971/mo | Organizations whose golden artifact working set or replication traffic dominates. |

`f4` is the default class for customers willing to pay for CI speed because
golden artifacts remove checkout, dependency download, and cache rehydration
from the hot path. Once cache hits dominate, customer wall-clock time is
primarily shaped by CPU frequency, available parallelism, memory headroom, and
local NVMe latency. `rs4` is selected by observed durable-cache working-set
pressure, ZFS replication pressure, or cache eviction rate. `m4` and Gen 3
`c3`/`s3` capacity are economy classes for workloads whose queueing priority or
price point matters more than elapsed time.

The `f4.metal.medium` commitment model:

| Commitment | Price | Hourly-equivalent over 730 h |
|---|---|---|
| on-demand hourly | $1.52 / hour | $1.52 |
| monthly commit | $555 / month | $0.760 |
| annual commit | $4,662 / year ($388.50 / month) | $0.532 |

The commitment crossover follows from these: monthly commit beats hourly above
`555 ÷ 1.52 ≈ 365` busy hours per month (≈50% duty cycle); annual beats hourly
above `388.50 ÷ 1.52 ≈ 256` hours per month (≈35% duty cycle). Below those duty
cycles, on-demand hourly is cheaper.

## Multi-resource break-even

A box is a resource vector. `f4.metal.medium` supplies 32 vCPU-threads, 128 GiB
memory, local NVMe governed by the site's ZFS layout, and a fixed NIC bandwidth
`b_nic`. Instantaneous revenue rate is the dot product of utilized resources
and the price vector; the box clears its cost when that rate meets the chosen
commitment's hourly-equivalent.

Resource utilizations are not independent: a workload occupies vCPU and memory
in a fixed ratio, and the box saturates on whichever resource that ratio
exhausts first. The binding resource is the workload's dominant resource
(Dominant Resource Fairness, Ghodsi et al., NSDI 2011). The `f4.metal.medium`
box ratio is `32 vCPU ÷ 128 GiB = 0.25` vCPU/GiB; a workload above 0.25
vCPU/GiB saturates compute first, below it saturates memory first. A
4 vCPU / 8 GiB CI job is 0.5 vCPU/GiB and is compute-bound, so compute
utilization is the controlling break-even variable for the current product.

Compute-bound break-even at the target compute price of $0.12/vCPU-hour,
full-box compute revenue `32 × $0.12 = $3.84/hour`:

| Commitment | Hourly-equivalent cost | Break-even compute utilization | Sustained vCPU |
|---|---|---|---|
| monthly commit | $0.760 | 19.8% | 6.3 of 32 |
| annual commit | $0.532 | 13.9% | 4.4 of 32 |
| on-demand hourly | $1.52 | 39.6% | 12.7 of 32 |

Memory, storage, and egress revenue from the same workloads are additive headroom above these thresholds; they lower the required compute utilization rather than competing with it, until a workload mix shifts the dominant resource to memory.

## Worker class selection

Runner classes map to host classes and customer-facing runner labels. The
default premium runner class targets `f4.metal.medium`; the flagship class
targets `f4.metal.large`; storage/cache-dense classes target `rs4.*`; economy
classes target `m4` or Gen 3 pools.

Observed runner behavior chooses the class:

- CPU-bound CI with hot golden artifacts goes to `f4`.
- Memory-heavy JVM, browser, integration-test, or database-backed suites go to
  `f4.metal.large` before `rs4` because high clock speed still reduces
  wall-clock time.
- Large durable-cache working sets, high cache churn, frequent cross-box
  `zfs send`/`recv`, or durable volume eviction go to `rs4`.
- Price-sensitive or backfill workloads go to `m4`, `c3`, or `s3`.
- GPU CI uses metal GPU only when the workflow explicitly needs CUDA or model
  acceleration.
- CPU VMs are reserved for control-plane or low-isolation utility workloads;
  customer CI workers use bare metal so Firecracker, ZFS, and scheduling
  latency stay predictable.

Runner class is a cache compatibility dimension. Moving a hot organization
between `f4`, `m4`, and `rs4` creates a distinct compatible working set unless
the scheduler prewarms the target box with replicated golden artifacts before
cutover.

## Capacity planning

The provisioning decision for a period is whether forecast sustained demand
keeps a box's dominant resource above the commitment break-even for that
period, and which commitment minimizes cost at the forecast duty cycle. An
`f4.metal.medium` box committed monthly must sustain ≥6.3 vCPU at the target
compute price across the month to clear cost; below a ~50% duty cycle the same
demand is served more cheaply on-demand hourly; an annual commitment is
justified only by demand that holds above ~35% duty cycle for a year. Forecast
is taken from the durable metering history (see open items), not from
instantaneous load, because the commitment is a period-length bet.

Each provisioned box is evaluated independently. A box resets to zero occupancy on provisioning and must reach its dominant-resource break-even within its own billing window; aggregate margin holds only if every committed box clears its own floor. Provisioning ahead of demand carries boxes below break-even, so capacity is added against a forecast of sustained demand rather than reactively against instantaneous demand.

## Capacity management

The marginal unit of supply is a bare-metal box with a multi-stage host-convergence lead time (Ansible bootstrap, ZFS pool, Nomad join, SPIRE attestation, ClickHouse schema). The marginal unit of demand is a sandbox lasting seconds to minutes. Bursts are absorbed from a warm headroom buffer of already-provisioned, already-converged capacity; provisioning is driven by sustained-demand forecast, not by individual bursts.

A newly provisioned box holds no golden artifacts, and sandboxes scheduled onto
it before its working set is warmed take the cold acquisition path that the
compute price exists to eliminate. Scaled-out capacity is brought into service
warm by replicating hot organizations' durable generations with `zfs send` and
`zfs recv` and staging their Firecracker vmstate/memory artifacts ahead of
cutover. Golden artifacts are per-`(org, repo, target-branch, workflow-id,
job-id, matrix-key)` state resident in one box's pool and snapshot store, so
scheduling is artifact-affinity constrained: a sandbox is placed where its
golden artifact resides or can be cloned cheaply from a local replica.

The control signal is dominant-resource utilization per box against the active commitment's break-even. Scale-out is predictive and hysteretic: a box is added when a sustained dominant-resource trend crosses threshold and persists, sized to clear its own floor on forecast load. Scale-in is conservative: a box is drained and deprovisioned only after prolonged idle and after its unique golden-artifact working set is replicated elsewhere or retired.

## The model as data

The contribution-margin model is a query, not a service. Cost comes from `verself.latitude_plan_prices`. Demand comes from the durable metering projection; raw resource-time is recovered by dividing component charge-units by the SKU's `unit_rate`. Revenue is recomputed from raw resource-time against the target price vector (or, in bundled mode, against the bundle rate), independent of the placeholder seed rates. Output is per-box contribution margin, dominant-resource utilization, and break-even distance per period, emitted as a ClickHouse artifact and read by the planning surface. Pricing mode and the price vector are query parameters.

## Open items

The demand input requires a durable metering history. `verself.billing_events` is an ephemeral live working set (windows roll off within a day), so capacity planning reads the durable metering projection emitted by `ProjectMeteringWindow`, not the event stream. The model is calibrated only once the rate card is set to the target price vector and the durable metering source is confirmed. Durable-storage and north/south-egress SKUs do not exist in the `sandbox-default` seed and are required before those resources contribute to the model.
