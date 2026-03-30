# Go TUI Alternatives

## Comparison Matrix

| Dimension | BubbleTea | tview | gocui (forks) | tcell | termbox-go | termui |
|-----------|-----------|-------|---------------|-------|------------|--------|
| **Stars** | 41,049 | 13,719 | 10,536 (orig) | 5,108 | 4,762 | 13,524 |
| **Architecture** | Elm/functional | Widget tree | View/window | Cell buffer | Cell buffer | Dashboard widgets |
| **Model** | Message/Update/View | Imperative OOP | Layout callback + keybinds | Immediate-mode cells | Immediate-mode cells | Widget placement |
| **Inline mode** | Yes | No | No | No | No | No |
| **Full-screen** | Yes | Yes | Yes | Yes | Yes | Yes |
| **Built-in widgets** | Via Bubbles (separate) | Rich (tables, forms, trees) | None | None | None | Charts, gauges |
| **Layout** | Manual / Lip Gloss | Grid + Flex | Absolute coords | Manual | Manual | Grid |
| **Overlapping views** | Via lipgloss layers (v2) | Limited | Yes (native) | N/A | N/A | No |
| **Styling** | Lip Gloss | Method chaining | Manual | Manual | Manual | Built-in |
| **Testing** | teatest + golden files | Extract logic manually | Extract logic manually | N/A | N/A | N/A |
| **Maintenance** | Active (funded company) | Active (solo) | Forks only | Active | Abandoned | Sporadic |
| **Renderer** | Own (Cursed Renderer v2) | tcell | tcell (forks) | Self | Self | termbox-go |

## tview (13,719 stars)

**Architecture:** Retained-mode widget tree. Construct tree of widget objects (`Box`,
`TextView`, `Table`, `Form`, `List`, `TreeView`, `Grid`, `Flex`, `Pages`), set
properties, call `Application.Run()`. Analogous to Qt/GTK/WinForms.

**Maintained by:** rivo (single maintainer). Last commit: 2026-03-16. Bus factor of 1.

**Strengths:**
- Rich built-in widget set -- tables, forms, tree views, text areas, dropdowns, images,
  modals all included out of the box
- Lower learning curve for developers from traditional GUI backgrounds
- Excellent for dashboard-style applications (K9s is the poster child)
- Stable API with strong backwards-compatibility commitment

**Weaknesses:**
- **Full-screen only -- no inline mode.** Confirmed in tview issue #1067: not possible
  with tcell's architecture. This is a dealbreaker for CLI tools that should render
  within terminal flow.
- Mutable widget state makes concurrent access tricky. Thread-safe only via
  `Application.QueueUpdate()`, easy to get wrong.
- Harder to test (no equivalent to teatest). Business logic must be extracted manually.
- No styling library equivalent to Lip Gloss
- Smaller ecosystem (no Huh, no Gum, no Wish integration)

**Notable production users:**
- K9s (33,209 stars) -- Kubernetes cluster management (uses private fork: `derailed/tview`)
- lazysql, podman-tui, gdu, viddy, wtfutil

**Best fit:** Complex full-screen dashboards with multiple panels, tables, forms where
you want lots of built-in widgets and a familiar imperative style.

### GitLab CLI migration: tview -> BubbleTea

GitLab CLI is migrating from tview to BubbleTea (epic #19748). Cited reasons:
- Better mouse support and window resize handling
- Accessibility support
- Single unified ecosystem vs 3 separate libraries
- Active, funded development team

This is a notable signal: the largest open-source project we found using tview for CLI
purposes is actively leaving it.

## tcell (5,108 stars)

**Not a TUI framework** -- it is the terminal abstraction layer that tview builds on.
Cell-based screen model: write characters to (x, y) coordinates with style attributes,
call `Show()` to flush.

Pure Go, no CGO. Excellent cross-platform support including WASM. Well-maintained (v3
current), 14 open issues. 24-bit color, bracketed paste, modern keyboard protocols.

BubbleTea v2 does NOT use tcell -- it has its own Cursed Renderer. This is a divergence
from v1 where some users bridged the two.

**Best fit:** Building your own TUI framework or needing pixel-level control. Not a
BubbleTea alternative -- different layer of the stack.

## gocui (10,536 stars original, fragmented)

**Architecture:** View-based windowing system. Named "views" (rectangular regions) with
absolute coordinates, keybindings per-view, `Manager.Layout()` callback called each frame.

**Fragmented into forks:**
- Original (`jroimartin/gocui`): effectively abandoned (last real commit 2021)
- `awesome-gocui/gocui`: community fork, active
- `jesseduffield/gocui`: maintained for lazygit (last commit 2026-03-27)

**Strengths:**
- Overlapping views supported natively (unlike BubbleTea pre-v2)
- Minimalist, small API surface
- jesseduffield fork is battle-tested (lazygit has 57k+ stars)

**Weaknesses:**
- Fork fragmentation confuses new users
- No built-in widgets (build everything yourself)
- Absolute positioning only (no flexbox/grid)
- Small ecosystem

**Notable users:** lazygit (57k+ stars), lazydocker, lazyjournal

**Best fit:** Multi-pane layouts with overlapping windows. jesseduffield fork if you
choose this path.

## termbox-go (4,762 stars)

**Abandoned.** Author explicitly recommends tcell as replacement. No Unicode grapheme
cluster support, no 24-bit color, no modern keyboard protocols. Do not use for new
projects.

## termui (13,524 stars)

**Dashboard widget library** built on termbox-go. Pre-built: line charts, bar charts,
sparklines, gauges, lists, tables, grids. Inspired by blessed-contrib (Node.js).

Built on abandoned termbox-go. Maintenance sporadic (months between commits).
Dashboard-only focus -- no forms, no text input, no general interactivity.

**Best fit:** Quick throwaway dashboards with charts. For production, tview's table +
tvxwidgets is a better path.

## Cross-language: Ratatui (Rust)

Worth noting for benchmarking context. In a test rendering 1,000 data points/second,
Ratatui used 30-40% less memory and 15% less CPU than BubbleTea. Go's GC overhead is
visible under sustained rendering pressure. For forge-metal's use case (dashboard
refreshing at 60fps with ~100 job rows), this gap is negligible.

## Recommendation for forge-metal

**BubbleTea is the clear winner** for `cmd/bmci/`:

1. **Inline mode** -- doctor checks, domain setup, benchmark output should render inline,
   not take over the screen. Only BubbleTea supports this.
2. **Huh forms** -- drop-in replacement for `TTYPrompter` with validation and theming.
3. **Testability** -- Elm Architecture makes state transitions unit-testable as pure functions.
4. **Ecosystem momentum** -- GitLab CLI migrating TO BubbleTea. Largest, most active community.
5. **v2 renderer** -- bandwidth optimization matters for SSH to Latitude.sh bare-metal hosts.

tview would be preferred only for a persistent full-screen dashboard (K9s-style). For
CLI tools with interactive prompts and streaming output, BubbleTea + Huh is the right stack.
