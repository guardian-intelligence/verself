# Charm Ecosystem

BubbleTea is the center of gravity. Every other library plugs into it, styles it,
or extends it.

## Core Libraries

### Bubbles (8,093 stars)

Reusable TUI components that implement `tea.Model`. Pure composition -- embed in your
model, forward messages in `Update`, call `View` in your view. No inheritance, no
registration.

| Component | Purpose |
|-----------|---------|
| Spinner | Animated loading indicators with customizable frames |
| Text Input | Single-line input with unicode, pasting, in-place scrolling |
| Text Area | Multi-line input (chat, editors) |
| Table | Column/row display with vertical scrolling |
| Progress | Progress bars with gradient fills, spring animation via Harmonica |
| Paginator | Dot-style or numeric pagination |
| Viewport | Vertical scrolling, high-performance mode for alt screen |
| List | Batteries-included: pagination, fuzzy filtering, help, spinner, status |
| File Picker | Filesystem navigation with extension filtering |
| Timer / Stopwatch | Countdown and count-up |
| Help | Auto-generated keybinding help views |
| Key | Non-visual keybinding management |

**v2 changes:** Getters/setters everywhere (replacing exported fields), coordinated
light/dark style support via `tea.BackgroundColorMsg`, import path `charm.land/bubbles/v2`.

**Third-party extensions:** `charm-and-friends/additional-bubbles` collects community
components not in the official library.

### Lip Gloss (10,938 stars)

CSS-inspired terminal styling and layout engine.

```go
style := lipgloss.NewStyle().
    Bold(true).
    Foreground(lipgloss.Color("#FAFAFA")).
    Padding(2, 4).
    Width(22)
```

**Capabilities:**
- Color: ANSI 16, ANSI 256, True Color (24-bit), 1-bit ASCII
- Auto-downsampling: colors degrade gracefully
- Color utilities: `Darken`, `Lighten`, `Complementary`, `Alpha`, gradient blending
- Block-level: padding, margins (CSS shorthand), alignment (left/right/center), width/height
- Borders: normal, rounded, thick, double, custom, with color gradients
- Compositing: `NewLayer(content).X(4).Y(2).Z(1)` for layered rendering with hit testing
- Tables: `charm.land/lipgloss/v2/table` sub-package
- Joining: `JoinHorizontal`, `JoinVertical` for composing text blocks
- Inline: bold, italic, faint, blink, strikethrough, underline variants, hyperlinks
- Style inheritance: `Inherit(otherStyle)` copies unset rules only
- Value types: assignment creates true copies (no aliasing bugs)

**v2 key change:** Lip Gloss is now pure/deterministic (no hidden I/O). BubbleTea
manages all terminal queries. Colors are standard `color.Color`. `AdaptiveColor`
removed; replaced by `LightDark(isDark bool)`.

#### Compositor deep dive

The compositor is a two-tier system:

- **`Layer`**: data struct holding content, x/y/z coordinates, optional children.
  Children's coordinates are relative to parent. Forms a tree.
- **`Compositor`**: takes layer tree, flattens to sorted list, renders.

**Composition model: opaque painter's algorithm (NOT alpha blending).** Layers are
drawn low-z to high-z. Higher-z layers overwrite cells completely. No alpha channel --
cells either have content or are empty.

**Hit testing is bounding-box, NOT cell-level.** `comp.Hit(x, y)` checks
`pt.In(cl.bounds)` where bounds is computed from content width/height. Clicking on
"transparent" (empty) parts of a layer still registers as a hit. Layers without an ID
are skipped. Top-most (highest z) layer with an ID wins.

**Performance:** O(n) flatten + O(n log n) sort + O(n * cells) draw. No dirty-region
tracking in the compositor itself. The underlying `RenderBuffer` has touched-line
tracking for terminal rendering. All layers drawn every `Render()` call. For 10-20
layers on a typical terminal: negligible. Hundreds of layers with large content: the
ANSI string parsing via `ansi.DecodeSequence` per layer is the bottleneck.

#### Color downsampling algorithm

Three-layer system:

1. **`colorprofile.Profile`** -- detects capability (NoTTY, ASCII, ANSI, ANSI256,
   TrueColor) via `$COLORTERM`, `$TERM`, terminal queries
2. **TrueColor -> ANSI 256** -- ported from tmux's `colour.c`. Maps to 6x6x6 cube
   (levels: `[0x00, 0x5f, 0x87, 0xaf, 0xd7, 0xff]`) or 24-grey ramp. Uses
   `colorful.DistanceHSLuv()` (perceptual distance in HSLuv space, not Euclidean RGB)
   to pick between cube color and grey. Better results for near-greys.
3. **ANSI 256 -> ANSI 16** -- static lookup table `ansi256To16[256]`.

Conversions are cached in a global RWMutex-protected `map[Profile]map[color.Color]color.Color`.

In v2, downsampling happens at **output time** (via `colorprofile.Writer` wrapping
stdout), not render time. `Style.Render()` always emits full-fidelity ANSI. This is
a key v2 change: v1 styles carried `*Renderer` pointers and downsampled during render.

#### Table sub-package internals

**No virtual scrolling.** `DataToMatrix()` materializes ALL rows. See
[performance.md](performance.md) for scaling implications.

**Column resizing** is median-based (not uniform):
1. Track min/max/median content widths per column across all rows
2. Expanding: widen shortest column by 1 pixel repeatedly until target width
3. Shrinking (three-phase): shrink columns >= half table width first, then shrink
   columns with biggest gap between actual width and median, then shrink widest remaining

This avoids naive uniform shrinking -- a column with header "Age of Person" (15 chars)
but data values of 2 chars gets shrunk first.

#### JoinHorizontal/JoinVertical

Float-based position (0.0-1.0) with proportional padding distribution. Uses
`ansi.StringWidth()` which is ANSI-aware and handles CJK/emoji double-width correctly.
Padding uses `strings.Repeat(" ", gap)` -- character-based, not pixel-based.

**Limitation:** does NOT handle ANSI escape sequences spanning multiple lines. Each
line is treated independently. Styles wrapping across newlines get split.

#### Cassowary constraint solver (ultraviolet, not yet surfaced)

`ultraviolet/layout/` contains a Cassowary-based constraint layout engine ported from
Ratatui. Constraint types: `Len`, `Min`, `Max`, `Percent`, `Ratio`, `Fill`. Flex modes:
`FlexStart`, `FlexEnd`, `FlexCenter`, `FlexSpaceEvenly`, `FlexSpaceAround`,
`FlexSpaceBetween`. LRU-cached with FNV hash keys.

Not yet exposed through lipgloss's public API -- it's the infrastructure for future
layout primitives. `JoinHorizontal`/`JoinVertical` remain the primary layout tools.

### Huh (6,722 stars)

Interactive forms and prompts. Terminal equivalent of web forms.

**Field types:** Input (single line), Text (multi-line), Select, MultiSelect (with limit),
Confirm (yes/no).

**Key features:**
- **Standalone or embedded:** `form.Run()` standalone, or embed as `tea.Model`
- **Dynamic forms:** `TitleFunc`, `OptionsFunc` with bindings -- fields recompute when
  upstream values change. Built-in caching for expensive computations.
- **Validation:** Per-field validators with inline error display
- **Theming:** 5 built-in (Charm, Dracula, Catppuccin, Base 16, Default), fully customizable
- **Accessibility mode:** `WithAccessible(true)` drops TUI for standard prompts (screen readers)
- **Spinner sub-package:** Background activity indicator after form submission
- **Each field has `.Run()`:** Quick single-prompt usage without building a full form

**Integration pattern:**
```go
// Huh form as child model
type model struct {
    form *huh.Form
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    form, cmd := m.form.Update(msg)
    if f, ok := form.(*huh.Form); ok {
        m.form = f
    }
    if m.form.State == huh.StateCompleted {
        name := m.form.GetString("name")
        // use result
    }
    return m, cmd
}
```

### Wish (5,079 stars)

SSH server framework. Built on `gliderlabs/ssh` (not OpenSSH). No shell shared by
default -- safe by design.

**Middleware architecture:**
- `bubbletea` middleware: serves any BubbleTea app over SSH. Each session gets its own
  `tea.Program` with PTY wired. Window resize propagates natively.
- `git` middleware: adds git server functionality
- `logging` middleware: connection logging
- `activeterm` middleware: reject non-terminal connections
- `accesscontrol` middleware: restrict allowed commands

```go
s, _ := wish.NewServer(
    wish.WithMiddleware(
        bubbletea.Middleware(teaHandler),
        activeterm.Middleware(),
        logging.Middleware(),
    ),
)
```

**Key insight:** SSH is treated as a protocol for serving *applications*, not remote
shells. The same BubbleTea app that runs locally can be served over SSH with minimal
changes. BubbleTea v2's Cursed Renderer reduces SSH bandwidth by orders of magnitude
compared to v1.

### Log (3,199 stars)

Minimal, colorful structured logging. Uses Lip Gloss for styling.

- Leveled: Debug, Info, Warn, Error, Fatal
- Structured key-value pairs: `log.Error("failed", "err", err)`
- Formatters: Text (colorful), JSON, Logfmt
- `slog.Handler` implementation
- Standard `*log.Logger` adapter for third-party libraries
- Auto-disables color when output is not a TTY

## Application-Level Tools

| Tool | Stars | What it does |
|------|-------|-------------|
| Glow | 24,033 | Markdown reader for terminal |
| Gum | 23,168 | Shell-scriptable BubbleTea components (prompts, spinners, tables in bash) |
| Crush | 22,172 | Agentic AI coding in terminal (launched May 2025) |
| VHS | 19,171 | Terminal session recorder (generates GIFs from scripts) |
| Soft-Serve | 6,752 | Self-hostable Git server with TUI over SSH |
| Mods | 4,513 | AI on the CLI (multi-provider) |
| Freeze | 4,405 | Code/terminal screenshot tool |
| Harmonica | 1,481 | Spring animation physics engine |
| Glamour | -- | Markdown rendering engine (powers Glow) |

### Soft-Serve architecture (relevant reference)

Self-hosted Git server accessible via SSH. The `UI` struct in `pkg/ssh/ui.go` demonstrates:
- Model tree: pages (selection, repo), header/footer, state machine (loading/error/ready)
- Lightweight task manager in `pkg/task/manager.go`: `Add(id, fn)`, `Run(id, done)`,
  `Stop(id)`, idempotent start (`atomic.Bool`), `sync.Map` for concurrent access
- Full Wish integration: each SSH session spawns a `tea.Program`

### Gum as shell glue

Gum wraps BubbleTea components for shell scripts:
```bash
NAME=$(gum input --placeholder "Project name")
CHOICE=$(gum choose "Option A" "Option B" "Option C")
gum spin --title "Deploying..." -- make deploy
```

This is relevant for forge-metal's Makefile targets and setup scripts -- Gum provides
interactive prompts without writing Go.

## Charm the Company

**Founded:** 2019 by Toby Padilla and Christian Rocha (New York)
**Funding:** $9.9M (Alphabet/Gradient Ventures, Firestreak Ventures, Niche Capital)

### Business model evolution

**Charm Cloud (sunsetted 2024-11-29):** Hosted backend for Charm apps (key-value store,
user accounts via SSH keys, encryption). Shut down to focus on higher-impact work. Code
remains open source and self-hostable via `charm serve`.

**Current direction:** The pivot toward Crush (agentic AI coding, 22K stars in <1 year)
suggests the company is betting on AI-powered developer tools as the commercial layer.
Crush supports multiple LLM providers (OpenAI, Anthropic, Google, Groq), has MCP
extensibility. The GitHub-like model (free for individuals, enterprise hosting) applies.

### Vision

Terminal-native applications as first-class software. Not just CLI tools, but rich
interactive applications delivered over SSH (Wish), styled with CSS-like semantics
(Lip Gloss), built with functional reactive architecture (BubbleTea). The terminal as
an application platform with better security properties (SSH auth, no HTTPS cert
management), universal access, and dramatically lower overhead than web apps.
