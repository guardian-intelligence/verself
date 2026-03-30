# BubbleTea Performance

## Rendering Pipeline

### v1: Line-based diffing

The standard renderer compares each line of `View()` output against the previous frame.
Unchanged lines are skipped (`canSkip` optimization, PR #95). Zero repaints when idle.

### v2: Cell-based diffing (Cursed Renderer)

ncurses-derived cell-diff algorithm in `ultraviolet`. Tracks dirty cells, only redraws
changed cells. Scroll optimization uses hashmap-based hunk detection + terminal scroll
commands (`SU`/`SD`) to physically move content instead of rewriting.

**v2 regression (issue #1481, open):** A complex app saw **2x performance degradation**
after v1→v2 migration. Profiling showed `cellbuf` + ANSI parsing making "tons of copies
every frame, more than 50% of all allocations." User's app ran 110-120 FPS on v1,
dropped to <60 FPS on v2. Zero maintainer response as of March 2026.

## Memory Usage

### Baseline

Go runtime carries ~8-12 MB baseline. A minimal BubbleTea app uses ~10-15 MB RSS.
Complex dashboards with lipgloss, viewports, large content: 40-80 MB.

### Viewport memory regression (bubbles issue #829, fixed)

Viewport + lipgloss consumed 20-40 MB for a ~1 MB EPUB file. Root cause:
`ansi.GetParser` allocated fresh 4KB parser instances on every call. **Fixed in bubbles
v0.21.1** (Feb 2026) with a parser pool.

## Table and List Scaling (Critical)

### Table: O(n) rendering per cursor move (bubbles issue #276)

| Row count | `MoveDown()` latency |
|-----------|---------------------|
| <1,000 rows, 6 cols | ~2ms |
| 4,500 rows, 6 cols | **300-500ms** |

Root cause: `UpdateViewport()` iterates and copies the **entire table** into
`renderedRows` on every cursor move. Every row is re-rendered with lipgloss styling.

**No virtual scrolling.** The table renders all rows, then the viewport shows a slice.

### List: Similar scaling (bubbles issue #810)

~8,000 items causes noticeable scroll delay. A PR (#882) reportedly improves this
but the specifics are unclear.

### Lip Gloss table: `DataToMatrix()` materializes ALL rows

Even with `Height(20)` set, the table sub-package calls `DataToMatrix(t.data)` which
converts the entire dataset to `[][]string` for width calculation:

```go
func (t *Table) resize() {
    rows := DataToMatrix(t.data)  // materializes ALL rows
    ...
}
```

A custom `Data` interface can defer per-cell access, but the resizer still iterates
all rows for column width statistics.

### Workarounds

1. **Manual virtual scrolling** -- maintain a window into your data, only pass visible
   rows + buffer to the table. Update window on scroll events.
2. **`Evertras/bubble-table`** -- third-party table with built-in pagination, filtering,
   sorting that reduces rendering surface.
3. **Pre-compute layouts** -- calculate lipgloss dimensions in `Update()` on resize,
   not in `View()`.

## BubbleTea vs Ratatui (Rust)

Benchmark: dashboard rendering 1,000 data points/second (Nov 2025):

| Metric | BubbleTea (Go) | Ratatui (Rust) |
|--------|----------------|----------------|
| Memory | Baseline | **30-40% less** |
| CPU | Baseline | **15% less** |
| GC pauses | Yes | None |

Primary driver: Rust's zero-cost abstractions and lack of GC. For forge-metal's
dashboard (~100 rows at 60fps), this gap is negligible. It matters for sustained
high-throughput rendering.

## SSH Bandwidth

- **v1:** rewrites entire changed lines. Kilobytes per frame over SSH for frequent updates.
- **v2:** only sends ANSI sequences for changed cells. Cursor movement optimization
  evaluates 4 methods per move and picks the shortest byte sequence.
- For mostly-static UI with small changing regions (status bar), v2 reduces SSH
  bandwidth 10-100x vs v1.

## Performance Antipatterns

1. **Work in `View()`** -- called every frame. Never do I/O, computation, or allocations
   beyond string building. All state changes belong in `Update()`.
2. **Blocking in `Update()`** -- blocks the entire event loop. Use `tea.Cmd` for async.
3. **Re-creating lipgloss styles every frame** -- `Style.Render()` allocates. Pre-create
   and reuse styles.
4. **Large table/list without pagination** -- >1,000 rows in default table = O(n) per
   cursor move. Must implement manual pagination.
5. **ANSI style bleeding across lines** -- the `canSkip` optimization compares raw
   strings. Styles spanning multiple lines cause incorrect skips (issue #1501). Apply
   styles per-line.
6. **Using `time.Sleep` in Update/View** -- blocks event loop. Use `tea.Tick`.

## Frame Dropping

BubbleTea has built-in frame skipping: if `Update()` + `View()` cycles happen faster
than the frame rate, intermediate frames are dropped. Only the latest `View()` output
is rendered at the next tick. Prevents the renderer from falling behind during burst
updates, but can cause visual artifacts when ANSI state carries across frames
(issue #1501).

## Key Numbers

| Metric | Value | Source |
|--------|-------|--------|
| Default FPS | 60 (max 120) | Source code |
| Table MoveDown at 1K rows | ~2ms | Issue #276 |
| Table MoveDown at 4.5K rows | ~300-500ms | Issue #276 |
| Viewport memory pre-fix | 20-40 MB for 1 MB content | Issue #829 |
| v2 vs v1 (complex app) | v2 ~2x slower (regression) | Issue #1481 |
| vs Ratatui memory | 30-40% more | Dev.to benchmark |
| vs Ratatui CPU | ~15% more | Dev.to benchmark |
| Go runtime baseline | ~10-15 MB RSS | General |
| Startup time | <10ms (framework itself) | Expected |
