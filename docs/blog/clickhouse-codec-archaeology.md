# DoubleDelta Is Lying to You

We run a single bare-metal box with ClickHouse storing OTel metrics from a hostmetrics scraper. About 15 million rows of CPU counters, disk I/O, memory gauges — the usual. The OTel ClickHouse exporter creates tables with reasonable-looking defaults, and for a while we didn't question them.

Then we looked at `system.columns` and found that our 90 MiB of compressed metrics data was paying a 30 MiB tax on a single column.

## The Starting Point

The OTel exporter's auto-created schema uses `ZSTD(1)` everywhere, plain `String` for columns like `MetricName` and `ScopeName`, and `Delta(8)` for timestamps. Pretty standard. We figured we'd tune the codecs, collect before/after numbers, and move on.

Here's what we expected to do:

- `String` → `LowCardinality(String)` for the repetitive OTel metadata columns (ScopeName, MetricName, MetricUnit — a handful of distinct values repeated millions of times)
- `ZSTD(1)` → `ZSTD(3)` everywhere (10-20% better, negligible write cost at batch sizes)
- `Delta(8)` → `DoubleDelta` for timestamps (the textbook move for evenly-spaced time series)
- `ZSTD(1)` → `Gorilla + ZSTD(3)` for float values (the textbook move for float time series)

The LowCardinality changes were massive. `ScopeName` went from 4.3 MiB to 57 KiB. That's a 98.7% reduction on one column. The ZSTD level bump helped. All expected.

Then the timestamp and float codecs came back *worse*.

## DoubleDelta Made TimeUnix 2x Bigger

We created shadow tables with identical data, identical merge state, and flipped one variable at a time:

| Codec | TimeUnix Compressed | Ratio |
|:------|--------------------:|------:|
| Delta(8) + ZSTD(3) | 45.9 MiB | 2.64x |
| DoubleDelta + ZSTD(3) | 59.6 MiB | 2.04x |

DoubleDelta was 30% *larger*. Same story for Gorilla on float values — 33% worse than plain ZSTD.

The sort order was the obvious suspect. These tables sort by `(ServiceName, MetricName, Attributes, TimeUnix)` — timestamp is the last key. Timestamps jump backwards at every series boundary. But we have ~115 distinct series with ~130K rows each. Each compressed block (8192 rows) sits entirely within one series. Boundary jumps happen every 16 blocks. That shouldn't be enough to tank DoubleDelta.

So we profiled the actual delta distribution.

## The Bimodal Signal

Our OTel hostmetrics process scraper matches 12 processes by regex. For metrics like `process.cpu.time`, each process gets its own data point per scrape, but they all share the same `(MetricName, Attributes={state:system})` key — the PID lives in `ResourceAttributes`, not `Attributes`. Twelve processes, microsecond-offset timestamps, one "series" in the sort order.

The physical-order delta sequence looks like this:

```
Δ = 33μs  (next process, same scrape)
Δ = 999ms (next scrape cycle)
Δ = 60μs  (next process, same scrape)
Δ = 34μs
...11 more micro-deltas...
Δ = 999ms (next scrape cycle)
```

84% of deltas are ~1 second. 16% are ~30 microseconds. Two modes.

Delta(8) produces a stream that alternates between two tight value clusters. ZSTD loves this — it's low entropy, highly repetitive structure.

DoubleDelta takes the second derivative, which spikes by ±999,000,000 at every mode transition. That's a full-width 30-bit value roughly every 6 rows. The variable-width bit packing in DoubleDelta's encoding allocates maximum-width slots for these spikes, and ZSTD can't undo the damage.

## The Double-Encoding Problem

We tested codecs with and without ZSTD to isolate the effect:

| Precision | Delta (no ZSTD) | DoubleDelta (no ZSTD) |
|:----------|----------------:|----------------------:|
| nanosecond | 123 MiB (1.0x) | **68 MiB (1.8x)** |
| second | 62 MiB (1.0x) | **2.4 MiB (25x)** |

Without ZSTD, DoubleDelta wins. Its internal bit packing actually compresses; Delta alone just transforms. DoubleDelta is doing its job.

But layered with ZSTD:

| Precision | Delta + ZSTD(3) | DoubleDelta + ZSTD(3) |
|:----------|----------------:|----------------------:|
| nanosecond | **47 MiB** | 60 MiB |
| second | 133 KiB | **119 KiB** |

Delta(8) produces output that's *more compressible* by ZSTD than DoubleDelta's pre-encoded output. DoubleDelta's variable-width encoding makes bit-packing decisions optimized for its own format that interfere with what ZSTD would prefer to see. Two compressors fighting over the same data.

DoubleDelta only wins with ZSTD at second precision, where the bimodal signal collapses (microsecond deltas become zero) and the second derivative is truly constant.

## Precision Dwarfs Codec Choice

The real surprise was this: switching from `DateTime64(9)` (nanosecond) to `DateTime64(3)` (millisecond) cut TimeUnix from 47 MiB to 6 MiB. An 8x improvement, no codec change required.

At nanosecond precision, the 1-second deltas are ~1,000,000,000 — a 30-bit value. After Delta(8), each Int64 still has meaningful data in 4 of 8 bytes. At millisecond precision, the deltas are ~1,000 — a 10-bit value. Six of every eight bytes are zeros. ZSTD compresses zero runs for free.

Our hostmetrics scraper runs at 1-second intervals. There is exactly zero information content in the nanosecond digits. We truncated to milliseconds and nothing broke — the OTel exporter is write-only, dashboards aggregate at second granularity, and different scrape cycles are always distinguishable at millisecond resolution.

## The Final Numbers

Three files. Two `CREATE TABLE` statements. No ALTERs, no compatibility shims.

| What changed | Before | After |
|:-------------|-------:|------:|
| Overall compression ratio | 66x | 102x |
| TimeUnix (projected at 15M rows) | ~30 MiB | ~2.5 MiB |
| ScopeName | 4.3 MiB | 57 KiB |
| MetricName | 4.1 MiB | 40 KiB |
| Migration files | 8 | 3 |

## What We'd Do Differently

Profile the data before choosing codecs. We assumed "timestamps = DoubleDelta" because that's what every ClickHouse optimization guide says. But those guides assume your data is sorted by time. OTel metrics are sorted by `(ServiceName, MetricName, Attributes, Time)`, and the OTel process scraper creates a bimodal delta distribution that no guide warned us about.

The tools for profiling this are just SQL. Compute the deltas your codec will see in physical storage order, bucket them, and look at the distribution. If it's bimodal, DoubleDelta will lose to Delta + ZSTD. If your timestamps have nanosecond precision but second-scale intervals, the precision is your biggest lever — not the codec.

ClickHouse makes this experimentation cheap. `CREATE TABLE ... AS SELECT` with different codecs, `OPTIMIZE FINAL`, compare `system.columns`. The whole investigation — shadow tables, six codec variants, three precision levels — took about an hour and used no data we couldn't regenerate from the next minute of scraping.
