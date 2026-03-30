# BubbleTea Patterns & Gotchas

## Parent-Child Model Communication

Three progressive levels of composition:

### Level 1: Top-Down Composition (most common)

Parent model owns child models as struct fields. Parent's `Update()` routes messages
to children and collects commands. Parent's `View()` calls children's `View()` and
composes with lipgloss.

```go
type mainModel struct {
    state   sessionState
    timer   timer.Model
    spinner spinner.Model
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmds []tea.Cmd
    var cmd tea.Cmd

    // Always forward tick messages regardless of focus
    switch msg := msg.(type) {
    case spinner.TickMsg:
        m.spinner, cmd = m.spinner.Update(msg)
        cmds = append(cmds, cmd)
    case timer.TickMsg:
        m.timer, cmd = m.timer.Update(msg)
        cmds = append(cmds, cmd)
    }
    return m, tea.Batch(cmds...)
}
```

**Critical pattern:** forward *all* messages to the focused child, and tick messages
to their owning child regardless of focus. From the `composable-views` example.

### Level 2: Model Stack

Independent models pushed/popped like a navigation stack. Communication through commands
only. Used when models represent independent screens rather than parts of one layout.

### Level 3: Hybrid

Cache frequently-used models as struct fields, use stack for screen transitions.
Soft-Serve's `UI` struct demonstrates this: pages (selection, repo), header/footer
components, state machine (loading/error/ready).

**Canonical guidance (GitHub Discussion #751):** Route messages in the top-level
`Update()`, potentially altering them as they flow down. Framework doesn't prescribe
a single pattern -- choose based on whether children are parts of one layout (top-down)
or independent screens (stack).

## Concurrency Patterns

### Pattern A: Program.Send() from external goroutine (simplest)

```go
p := tea.NewProgram(newModel())
go func() {
    for result := range externalChannel {
        p.Send(resultMsg(result))  // thread-safe
    }
}()
p.Run()
```

### Pattern B: Subscription via channel + waitForActivity (Elm-style)

```go
func waitForActivity(sub chan struct{}) tea.Cmd {
    return func() tea.Msg {
        return responseMsg(<-sub)  // blocks in command goroutine
    }
}

// In Update, on responseMsg:
//   return m, waitForActivity(m.sub)  // re-subscribe
```

### Pattern C: tea.Tick for polling

```go
func tick() tea.Cmd {
    return tea.Tick(time.Second, func(t time.Time) tea.Msg {
        return tickMsg(t)
    })
}
```

### Batch vs Sequence composition

```go
// Two sequential pipelines running concurrently
tea.Batch(
    tea.Sequence(cloneZvol, bootVM, runJob),   // pipeline 1
    tea.Sequence(cloneZvol2, bootVM2, runJob2), // pipeline 2
)
```

### Debouncing (tag-based pattern)

From the `debounce` example: increment a counter on each keystroke, embed counter value
in the delayed message. When message arrives, check if tag matches model's current tag.
If not, ignore (a newer keystroke superseded it).

### Command goroutine lifecycle

Commands still running when the program exits are **intentionally leaked**. Long-running
commands should monitor `context.Context` for cancellation.

## Running External Commands

### tea.ExecProcess

Pauses the entire BubbleTea program, gives stdin/stdout to the subprocess, resumes on exit:

```go
func openEditor() tea.Cmd {
    editor := cmp.Or(os.Getenv("EDITOR"), "vim")
    c := exec.Command(editor)
    return tea.ExecProcess(c, func(err error) tea.Msg {
        return editorFinishedMsg{err}
    })
}
```

### Suspend/Resume

v2 supports `tea.Suspend` (sends SIGTSTP) and `tea.ResumeMsg` for ctrl+z behavior,
separate from exec.

## Window Size and Responsive Layouts

`tea.WindowSizeMsg` sent automatically on startup and on every terminal resize.

```go
case tea.WindowSizeMsg:
    m.width = msg.Width
    m.height = msg.Height
```

**Best practice:** calculate remaining space dynamically:
```go
contentHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
```

`tea.RequestWindowSize` forces a manual size query.

## Testing

### Option A: teatest (official)

```go
func TestApp(t *testing.T) {
    tm := teatest.NewTestModel(t, initialModel(),
        teatest.WithInitialTermSize(300, 100))

    teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
        return bytes.Contains(bts, []byte("Ready"))
    })

    tm.Send(tea.KeyPressMsg{Code: 'q'})

    // Golden file assertion
    out, _ := io.ReadAll(tm.FinalOutput(t))
    teatest.RequireEqualOutput(t, out)  // run with -update to regenerate

    // Or model state assertion
    fm := tm.FinalModel(t)
    m := fm.(model)
    assert.Equal(t, expected, m.field)
}
```

**Golden file gotchas:**
- Local golden files may fail in CI due to different color profiles. Fix:
  `lipgloss.SetColorProfile(termenv.Ascii)` in `func init()`.
- Add `*.golden -text` to `.gitattributes` to prevent Git mangling.
- `WithInitialTermSize` / `tea.WithWindowSize` is critical for deterministic output.

### Option B: Direct model testing (no tea.Program)

```go
func TestUpdate(t *testing.T) {
    m := initialModel()
    m, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
    assert.NotNil(t, cmd)  // should return tea.Quit
}
```

Simpler and fully deterministic. Use for unit-testing logic. Use teatest for
integration/golden-file tests.

### Option C: catwalk (third-party)

`github.com/knz/catwalk` -- unit test framework verifying state transitions and views
as messages are processed.

## Mouse Click Zones

### v2: Native Lipgloss Layers + Compositor

```go
func (m model) View() tea.View {
    var v tea.View

    bg := lipgloss.Place(m.width, m.height, lipgloss.Top, lipgloss.Left, body)
    root := lipgloss.NewLayer(bg).ID("bg")
    for i, d := range m.dialogs {
        root.AddLayers(d.view().Z(i + 1))
    }

    comp := lipgloss.NewCompositor(root)

    v.MouseMode = tea.MouseModeAllMotion
    v.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
        return func() tea.Msg {
            mouse := msg.Mouse()
            if id := comp.Hit(mouse.X, mouse.Y).ID(); id != "" {
                return LayerHitMsg{ID: id, Mouse: msg}
            }
            return nil
        }
    }
    v.SetContent(comp.Render())
    return v
}
```

Replaces the v1-era third-party `bubblezone` library. Layers have IDs,
`comp.Hit(x, y)` resolves which layer was clicked.

### v1: BubbleZone (third-party)

`github.com/lrstanley/bubblezone` -- wraps rendered text with invisible markers,
resolves which zone a mouse click hit. Still works for v1 codebases.

## Common Pitfalls

### Never block in Update() or View()

These run on the single event loop thread. Any blocking call freezes the entire UI.

```go
// BAD
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    time.Sleep(time.Minute)  // freezes UI for 1 minute
    return m, nil
}

// GOOD
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    return m, func() tea.Msg {
        time.Sleep(time.Minute)  // runs in goroutine
        return doneMsg{}
    }
}
```

### Never modify the model from a goroutine

Commands run concurrently. Communicate back via messages only.

```go
// BAD: race condition
cmd := func() tea.Msg {
    m.data = expensiveComputation()  // UNSAFE
    return nil
}

// GOOD
cmd := func() tea.Msg {
    return resultMsg{data: expensiveComputation()}
}
```

### Value receivers vs pointer receivers

Official examples use value receivers (functional Elm style). Pointer receivers avoid
copying large models but introduce risk of mutations outside `Update()`. Community is
split -- pointer receivers require discipline.

### Panic recovery

Panics inside commands can leave terminal in raw mode with hidden cursor. Recovery:
run `reset` in terminal. BubbleTea catches panics in commands and main loop, but not
bulletproof.

### stdout is occupied

Cannot use `fmt.Println` for debugging. Use `tea.LogToFile("debug.log", "prefix")`
and `tail -f debug.log` in another terminal.

### tea.Println only works in inline mode

If altscreen is active, `tea.Println` output is silently dropped. These commands print
persistent lines above the inline TUI (package-manager-style output).

### Message ordering from concurrent commands is non-deterministic

Use `tea.Sequence()` when ordering matters. Or design your protocol to handle
out-of-order delivery.

### Getting results after program exits

`p.Run()` returns the final `tea.Model`. Type-assert to your concrete type:
```go
m, _ := p.Run()
if result, ok := m.(model); ok {
    fmt.Println("Choice:", result.choice)
}
```

### Prevent quit with unsaved changes

Use `tea.WithFilter()` to intercept `tea.QuitMsg` and conditionally suppress it.
See `prevent-quit` example.

### Focus/blur detection

Set `v.ReportFocus = true` in View, handle `tea.FocusMsg` and `tea.BlurMsg` in Update.

## Debugging

```go
f, err := tea.LogToFile("debug.log", "debug")
if err != nil {
    log.Fatal(err)
}
defer f.Close()
```

For deep message-level debugging:
```go
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if m.dump != nil {
        spew.Fdump(m.dump, msg)
    }
    // ...
}
```

`TEA_LOGFILE` environment variable is checked by several examples.

## Open Issues Worth Watching

| Issue | Topic | Comments |
|-------|-------|----------|
| #163 | Displaying images inline | 17 |
| #573 | Altscreen resize artifacts | 13 |
| #309 | Compositing (mostly resolved in v2 via lipgloss layers) | 10 |
| #1017 | Viewport visible area incorrect when content exceeds width | 10 |
| #874 | IME input position | 7 |
| #162 | Native text selection + mouse wheel | 7 |
| #1244 | Two-way background goroutine communication example | 7 |
