# BubbleTea Architecture

## The Elm Architecture in Go

BubbleTea is a direct port of The Elm Architecture (TEA) to Go. The entire framework
revolves around a single interface:

```go
type Model interface {
    Init() Cmd
    Update(Msg) (Model, Cmd)
    View() View
}
```

**Model** -- any Go struct holding application state. Treated as immutable by the
framework: `Update` returns a *new* model value each time.

**Init()** -- called once at program start. Returns an optional `Cmd` for initial I/O
or `nil` for no-op.

**Update(Msg) (Model, Cmd)** -- central event handler. Receives a `Msg` (which is `any`
via the `ultraviolet` library). Uses type switches to dispatch. Returns updated model +
optional command. Called synchronously on the event loop -- never concurrent.

**View() View** -- pure rendering function. In v2, returns a `tea.View` struct (not a
plain string as in v1). The View struct is declarative: `Content`, `AltScreen`,
`MouseMode`, `Cursor`, `WindowTitle`, `ReportFocus`, `KeyboardEnhancements`,
`BackgroundColor`, `ForegroundColor`, etc.

### The critical design property

The event loop in `tea.go` (lines 724-864) is a single `for/select` that reads from
`p.msgs`, calls `model.Update(msg)`, sends the returned command to the command channel,
and calls `p.render(model)`. This guarantees model mutations are **single-threaded and
sequential** -- no mutexes needed in application code.

## The Program Type

`Program` wires together input, output, the event loop, and the renderer.

```go
p := tea.NewProgram(model, opts...)
```

### Key program options

| Option | Purpose |
|--------|---------|
| `WithContext(ctx)` | External cancellation |
| `WithInput(io.Reader)` / `WithOutput(io.Writer)` | Override stdin/stdout |
| `WithEnvironment([]string)` | For SSH/remote sessions |
| `WithFilter(func(Model, Msg) Msg)` | Message interception/filtering |
| `WithFPS(int)` | Custom render FPS (default 60, capped at 120) |
| `WithColorProfile(colorprofile.Profile)` | Force color profile |
| `WithoutRenderer()` | Disable rendering (daemon/headless mode) |
| `WithoutSignalHandler()` | Handle signals yourself |
| `WithoutCatchPanics()` | Disable panic recovery |
| `WithoutSignals()` | Ignore OS signals (useful for testing) |
| `WithWindowSize(w, h)` | Set initial window size (testing) |

### Program lifecycle (`Program.Run()`)

```
1. Set up input (stdin or TTY fallback)
2. Register signal handlers (SIGINT -> InterruptMsg, SIGTERM -> QuitMsg)
3. Initialize terminal (raw mode)
4. Get initial window size -> send WindowSizeMsg
5. Detect color profile -> send ColorProfileMsg
6. Send EnvMsg with environment variables
7. Initialize input reader + scanner
8. Start the renderer (ticker-based, default 60fps)
9. Query terminal capabilities (synchronized output mode 2026, unicode core mode 2027)
10. Call model.Init() and dispatch its returned command
11. Render initial view
12. Start resize handler
13. Start command handler goroutine
14. Enter the event loop (eventLoop)
15. On exit: render final state, restore terminal, shut down all handlers
```

### External API

- `p.Send(msg)` -- inject messages from outside (thread-safe, non-blocking)
- `p.Quit()` -- sends `QuitMsg`
- `p.Kill()` -- immediate shutdown, skips final render
- `p.Wait()` -- blocks until program fully exits
- `p.Run()` -- returns final `tea.Model` (type-assert to get result values)

## Messages and Commands

**Msg** is `type Msg = uv.Event` (effectively `any`). Messages are the sole mechanism
for state change.

### Built-in message types

| Category | Types |
|----------|-------|
| Keyboard | `KeyPressMsg`, `KeyReleaseMsg` |
| Mouse | `MouseClickMsg`, `MouseReleaseMsg`, `MouseWheelMsg`, `MouseMotionMsg` |
| Window | `WindowSizeMsg` |
| Terminal | `ColorProfileMsg`, `KeyboardEnhancementsMsg`, `ModeReportMsg`, `CapabilityMsg` |
| Lifecycle | `QuitMsg`, `InterruptMsg`, `SuspendMsg`, `ResumeMsg` |
| Clipboard | `ClipboardMsg`, `PasteMsg`, `PasteStartMsg`, `PasteEndMsg` |
| Focus | `FocusMsg`, `BlurMsg` |
| Environment | `EnvMsg` |

### Cmd

`type Cmd func() Msg` -- a function that performs I/O and returns a message. Executed
in a goroutine by the command handler. `nil` means no-op.

### Batch vs Sequence

**`tea.Batch(cmds ...Cmd)`** -- returns `BatchMsg` (`[]Cmd`). Executes all commands
**concurrently** via goroutines with `sync.WaitGroup`. No ordering guarantees.

**`tea.Sequence(cmds ...Cmd)`** -- returns `sequenceMsg` (`[]Cmd`). Executes commands
**one at a time, in order**. Each command's result is sent before the next runs.

They compose: `tea.Batch(tea.Sequence(a, b), tea.Sequence(c, d))` runs two sequential
pipelines in parallel.

Both use `compactCmds[T]` internally: nil commands stripped, single command returned
unwrapped.

### Timer commands

- `tea.Every(duration, fn)` -- ticks synchronized to system clock (wall-clock aligned)
- `tea.Tick(duration, fn)` -- ticks from invocation time
- Both are single-shot; re-issue in `Update` to create recurring ticks

### Special commands

| Command | Purpose |
|---------|---------|
| `tea.Quit` | `func() Msg { return QuitMsg{} }` |
| `tea.Suspend` / `tea.Interrupt` | Process control |
| `tea.SetClipboard` / `tea.ReadClipboard` | OSC52 clipboard |
| `tea.RequestWindowSize` | Force a size query |
| `tea.Println` / `tea.Printf` | Insert text above inline TUI into scrollback |
| `tea.ExecProcess(cmd, fn)` | Pause TUI, hand terminal to subprocess, resume on exit |
| `tea.RequestCapability(name)` | Query terminal capability |
| `tea.RequestBackgroundColor` | Detect light/dark mode |

## Renderer Architecture (v2: "Cursed Renderer")

Built from the ground up based on the ncurses rendering algorithm.

### Key properties

- **Cell-based rendering** -- operates on a cell grid, not raw strings
- **Ticker-driven** -- renders at configurable FPS via `time.Ticker`
- **Synchronized output (mode 2026)** -- automatically queries terminal, enables atomic
  updates to reduce tearing. Enabled by default.
- **Unicode core (mode 2027)** -- proper wide character/emoji handling
- **Color downsampling** -- via `charmbracelet/colorprofile`, auto-degrades ANSI colors
  to match terminal capabilities (TrueColor -> 256 -> 16 -> monochrome)
- **Optimization:** hard tabs and backspace for cursor movement; NL mapping for non-TTY

### Two rendering modes

**Inline mode** -- renders within terminal scrollback (default). Supports `tea.Println`
to insert lines above the TUI.

**Alt screen mode** -- full-window rendering. Declared via `view.AltScreen = true`.
In v2 this is declarative (was imperative command in v1).

### nilRenderer

Used when `WithoutRenderer()` is set. All render calls become no-ops. The Elm event
loop still runs -- same state machine, no visual output.

## Dependencies

| Package | Purpose |
|---------|---------|
| `charmbracelet/colorprofile` | Terminal color profile detection + auto-downsampling |
| `charmbracelet/ultraviolet` | Event types (`Msg`), terminal reader/scanner, environment |
| `charmbracelet/x/ansi` | ANSI escape sequence generation |
| `charmbracelet/x/term` | Terminal raw mode, size detection, TTY operations |
| `lucasb-eyer/go-colorful` | Color space conversions |
| `muesli/cancelreader` | Cancellable stdin reader (Go's `os.Stdin.Read` blocks) |
| `golang.org/x/sys` | Low-level syscalls (signals, terminal ioctl) |
| `rivo/uniseg` (indirect) | Unicode grapheme segmentation |

### Architectural note on ultraviolet

In v2, the `Msg` type is aliased to `uv.Event` from `charmbracelet/ultraviolet`.
Ultraviolet handles input scanning, event types, and environment abstraction. This
decouples terminal I/O from the framework -- in v1, all these types lived in bubbletea
itself.

## v2.0.0 Breaking Changes Summary

| Change | v1 | v2 |
|--------|----|----|
| Import path | `github.com/charmbracelet/bubbletea` | `charm.land/bubbletea/v2` |
| `View()` return | `string` | `tea.View` struct |
| Renderer | String-based diffing | Cell-based ncurses algorithm |
| Key messages | `tea.KeyMsg` with `.Type`/`.Runes` | `KeyPressMsg`/`KeyReleaseMsg` with `.Code`/`.Text`/`.Mod` |
| Mouse messages | Single `MouseMsg` | `MouseClickMsg`, `MouseReleaseMsg`, `MouseWheelMsg`, `MouseMotionMsg` |
| Alt screen | `tea.EnterAltScreen` command | `view.AltScreen = true` |
| Mouse mode | `tea.EnableMouseAllMotion` command | `view.MouseMode = tea.MouseModeAllMotion` |
| Lip Gloss | Owns terminal I/O | Pure (no I/O); BubbleTea manages all terminal state |
| Color handling | Manual | Automatic downsampling via `colorprofile` |
| Clipboard | External library | Built-in OSC52 |
| Keyboard | Basic | Progressive enhancements (Kitty protocol) |
| Space bar match | `case " ":` | `case "space":` |
| Ctrl keys | `tea.KeyCtrlC` constant | `msg.Code == 'c' && msg.Mod == tea.ModCtrl` |

## Version Timeline

| Version | Date | Notes |
|---------|------|-------|
| v2.0.2 | 2026-03-09 | Renderer fix for Wish (SSH) on Unix |
| v2.0.1 | 2026-03-02 | Fix default stdin file for input |
| **v2.0.0** | **2026-02-24** | **Major release** |
| v2.0.0-rc.2 | 2025-11-17 | Synchronized output enabled by default |
| v2.0.0-rc.1 | 2025-11-04 | Module name change; message types from aliases to structs |
| v2.0.0-beta.1 | 2025-03-26 | First v2 beta |
| v2.0.0-alpha.1 | 2024-09-18 | First v2 alpha |
| v1.3.10 | 2025-09-17 | Last v1.x maintenance release |
