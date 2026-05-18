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

Worker plan is Latitude `s3-large-x86` (`src/tools/provisioning/terraform/variables.tf`): ~24 cores / 48 threads, 128 GiB, ~1 TB sellable NVMe after OS, ZFS overhead, and pool slack. Live pricing is captured in `verself.latitude_plan_prices` (snapshot of the Latitude `/plans` catalog, USD per plan per site):

| Commitment | Price | Hourly-equivalent over 730 h |
|---|---|---|
| on-demand hourly | $1.78 / hour | $1.78 |
| monthly commit | $650 / month | $0.890 |
| annual commit | $5,460 / year ($455 / month) | $0.623 |

The commitment crossover follows from these: monthly commit beats hourly above `650 ÷ 1.78 ≈ 365` busy hours per month (≈50% duty cycle); annual beats hourly above `455 ÷ 1.78 ≈ 256` hours per month (≈35% duty cycle). Below those duty cycles, on-demand hourly is cheaper.

## Multi-resource break-even

A box is a resource vector. `s3-large-x86` supplies 48 vCPU-threads, 128 GiB memory, ~1 TB durable NVMe, and a fixed NIC bandwidth `b_nic`. Instantaneous revenue rate is the dot product of utilized resources and the price vector; the box clears its cost when that rate meets the chosen commitment's hourly-equivalent.

Resource utilizations are not independent: a workload occupies vCPU and memory in a fixed ratio, and the box saturates on whichever resource that ratio exhausts first. The binding resource is the workload's dominant resource (Dominant Resource Fairness, Ghodsi et al., NSDI 2011). The box ratio is `48 vCPU ÷ 128 GiB = 0.375` vCPU/GiB; a workload above 0.375 vCPU/GiB saturates compute first, below it saturates memory first. A 4 vCPU / 8 GiB CI job is 0.5 vCPU/GiB and is compute-bound, so compute utilization is the controlling break-even variable for the current product.

Compute-bound break-even at the target compute price of $0.12/vCPU-hour, full-box compute revenue `48 × $0.12 = $5.76/hour`:

| Commitment | Hourly-equivalent cost | Break-even compute utilization | Sustained vCPU |
|---|---|---|---|
| monthly commit | $0.890 | 15.4% | 7.4 of 48 |
| annual commit | $0.623 | 10.8% | 5.2 of 48 |
| on-demand hourly | $1.78 | 30.9% | 14.8 of 48 |

Memory, storage, and egress revenue from the same workloads are additive headroom above these thresholds; they lower the required compute utilization rather than competing with it, until a workload mix shifts the dominant resource to memory.

## Capacity planning

The provisioning decision for a period is whether forecast sustained demand keeps a box's dominant resource above the commitment break-even for that period, and which commitment minimizes cost at the forecast duty cycle. A box committed monthly must sustain ≥7.4 vCPU (at target price) across the month to clear cost; below a ~50% duty cycle the same demand is served more cheaply on-demand hourly; an annual commitment is justified only by demand that holds above ~35% duty cycle for a year. Forecast is taken from the durable metering history (see open items), not from instantaneous load, because the commitment is a period-length bet.

Each provisioned box is evaluated independently. A box resets to zero occupancy on provisioning and must reach its dominant-resource break-even within its own billing window; aggregate margin holds only if every committed box clears its own floor. Provisioning ahead of demand carries boxes below break-even, so capacity is added against a forecast of sustained demand rather than reactively against instantaneous demand.

## Capacity management

The marginal unit of supply is a bare-metal box with a multi-stage host-convergence lead time (Ansible bootstrap, ZFS pool, Nomad join, SPIRE attestation, ClickHouse schema). The marginal unit of demand is a sandbox lasting seconds to minutes. Bursts are absorbed from a warm headroom buffer of already-provisioned, already-converged capacity; provisioning is driven by sustained-demand forecast, not by individual bursts.

A newly provisioned box holds no golden zvols, and sandboxes scheduled onto it before its working set is warmed take the cold checkout path that the compute price exists to eliminate. Scaled-out capacity is brought into service warm by replicating hot organizations' golden zvols to buffer boxes with `zfs send`/`recv` ahead of cutover. Golden zvols are per-`(org, repo, target-branch, workflow-id, job-id, matrix-key)` state resident in one box's pool, so scheduling is zvol-affinity constrained: a sandbox is placed where its golden zvol resides or can be cloned cheaply from a local replica.

The control signal is dominant-resource utilization per box against the active commitment's break-even. Scale-out is predictive and hysteretic: a box is added when a sustained dominant-resource trend crosses threshold and persists, sized to clear its own floor on forecast load. Scale-in is conservative: a box is drained and deprovisioned only after prolonged idle and after its unique golden-zvol working set is replicated elsewhere or retired.

## The model as data

The contribution-margin model is a query, not a service. Cost comes from `verself.latitude_plan_prices`. Demand comes from the durable metering projection; raw resource-time is recovered by dividing component charge-units by the SKU's `unit_rate`. Revenue is recomputed from raw resource-time against the target price vector (or, in bundled mode, against the bundle rate), independent of the placeholder seed rates. Output is per-box contribution margin, dominant-resource utilization, and break-even distance per period, emitted as a ClickHouse artifact and read by the planning surface. Pricing mode and the price vector are query parameters.

## Open items

The demand input requires a durable metering history. `verself.billing_events` is an ephemeral live working set (windows roll off within a day), so capacity planning reads the durable metering projection emitted by `ProjectMeteringWindow`, not the event stream. The model is calibrated only once the rate card is set to the target price vector and the durable metering source is confirmed. Durable-storage and north/south-egress SKUs do not exist in the `sandbox-default` seed and are required before those resources contribute to the model.
