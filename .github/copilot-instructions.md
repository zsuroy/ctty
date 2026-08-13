# GitHub Copilot Instructions for Go + Bubble Tea (TUI)

These project-level instructions tell Copilot how to generate idiomatic, production-quality Go code using the Bubble Tea ecosystem. **Follow and prefer these rules over generic patterns.**

---

## 1) Project Scope & Goals

* Build terminal UIs with **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** and **Bubbles** components.
* Use **Lip Gloss** for styling and **Huh**/**Bubbles forms** for prompts where useful.
* Favor **small, composable models** and **message-driven state**.
* Prioritize **maintainability, testability, and clear separation** of update vs. view.

---

## 2) Go Conventions to Prefer

* Go version: **1.22+**.
* Module: `go.mod` with minimal, pinned dependencies; use `go get -u` only deliberately.
* Code style: `gofmt`, `go vet`, `staticcheck` (when available), `golangci-lint`.
* Names: short, meaningful; exported symbols require GoDoc comments.
* Errors: return wrapped errors with `%w` and `errors.Is/As`. No panics for flow control.
* Concurrency: use `context.Context` and `errgroup` where applicable. Avoid goroutine leaks; cancel contexts in `Quit`/`Stop`.
* Testing: `*_test.go`, table-driven tests, golden tests for `View()` when helpful.
* Logging: prefer structured logs (e.g., `slog`) and keep logs separate from UI rendering.

---

## 3) Bubble Tea Architecture Rules

### 3.1 Model layout

```go
// Model holds all state needed to render and update.
type Model struct {
    width, height int
    ready        bool

    // Domain state
    items   []Item
    cursor  int
    err     error

    // Child components
    list    list.Model
    spinner spinner.Model

    // Styles
    styles  Styles
}
```

**Guidelines**

* Keep **domain state** (data) separate from **UI components** (Bubbles models) and **styles**.
* Add a `Styles` struct to centralize Lip Gloss styles; initialize once.
* Track terminal size (`width`, `height`); re-calc layout on `tea.WindowSizeMsg`.

### 3.2 Init

* Return **batch** of startup commands for IO (e.g., loading data) and component inits.
* Never block in `Init`; do IO in `tea.Cmd`s.

```go
func (m Model) Init() tea.Cmd {
    return tea.Batch(m.spinner.Tick, loadItemsCmd())
}
```

### 3.3 Update

* **Pure function** style: transform `Model` + `Msg` → `(Model, Cmd)`.
* Always handle `tea.WindowSizeMsg` to set `m.width`/`m.height` and recompute layout.
* Use **type-switched** message handling; push side effects into `tea.Cmd`s.
* Bubble components: call `Update(msg)` on children and **return their Cmd**.

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width, m.height = msg.Width, msg.Height
        m.styles = NewStyles(m.width) // recompute if responsive
        return m, nil

    case errMsg:
        m.err = msg
        return m, nil

    case itemsLoaded:
        m.items = msg
        return m, nil
    }

    // delegate to children last
    var cmd tea.Cmd
    m.spinner, cmd = m.spinner.Update(msg)
    return m, cmd
}
```

### 3.4 View

* **Never** mutate state in `View()`.
* Compose layout with Lip Gloss; gracefully handle small terminals.
* Put errors and help at the bottom.

```go
func (m Model) View() string {
    if !m.ready {
        return m.styles.Loading.Render(m.spinner.View() + " Loading…")
    }
    main := lipgloss.JoinVertical(lipgloss.Left,
        m.styles.Title.Render("My App"),
        m.list.View(),
    )
    if m.err != nil {
        main += "\n" + m.styles.Error.Render(m.err.Error())
    }
    return m.styles.App.Render(main)
}
```

### 3.5 Messages & Commands

* Define **typed messages** for domain events, not raw strings.
* Each async operation returns a **message type**; handle errors with a dedicated `errMsg`.

```go
type itemsLoaded []Item

type errMsg error

func loadItemsCmd() tea.Cmd {
    return func() tea.Msg {
        items, err := fetchItems()
        if err != nil { return errMsg(err) }
        return itemsLoaded(items)
    }
}
```

### 3.6 Keys & Help

* Centralize keybindings and help text. Prefer `bubbles/key` + `bubbles/help`.

```go
type keyMap struct {
    Up, Down, Select, Quit key.Binding
}

var keys = keyMap{
    Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
    Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
    Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
    Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}
```

Handle keys in `Update` using `key.Matches(msg, keys.X)` and show a `help.Model` in the footer.

### 3.7 Submodels (Component Composition)

* For complex screens, create **submodels** with their own `(Model, Init, Update, View)` and wire them into a parent.
* Exchange messages via **custom Msg types** and/or **parent state**.
* Keep submodels **pure**; IO still goes through parent-level `tea.Cmd`s or via submodel commands returned to parent.

### 3.8 Program Options

* Start programs with `tea.NewProgram(m, tea.WithOutput(os.Stdout), tea.WithAltScreen())` when full-screen; avoid AltScreen for simple tools.
* Always handle **TTY absence** (e.g., piping); fall back to non-interactive.

---

## 4) Styling with Lip Gloss

* Maintain a single `Styles` struct with named styles.
* Compute widths once per resize; avoid per-cell Lip Gloss allocations in tight loops.
* Use `lipgloss.JoinHorizontal/Vertical` for layout; avoid manual spacing where possible.

```go
type Styles struct {
    App, Title, Error, Loading lipgloss.Style
}

func NewStyles(width int) Styles {
    return Styles{
        App:     lipgloss.NewStyle().Padding(1),
        Title:   lipgloss.NewStyle().Bold(true),
        Error:   lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
        Loading: lipgloss.NewStyle().Faint(true),
    }
}
```

---

## 5) IO, Concurrency & Performance

* **Never** perform blocking IO in `Update` directly; always return a `tea.Cmd` that does the work.
* Use `context.Context` inside commands; respect cancellation when program exits.
* Be careful with **goroutine leaks**: ensure commands stop when model quits.
* Batch commands with `tea.Batch` to keep updates snappy.
* For large lists, prefer `bubbles/list` with virtualization; avoid generating huge strings per frame.
* Debounce high-frequency events (typing) with timer-based commands.

---

## 6) Error Handling & UX

* Represent recoverable errors in the UI; do not exit on first error.
* Use `errMsg` for async failures; show a concise, styled error line.
* For fatal initialization errors, return `tea.Quit` with an explanatory message printed once.

---

## 7) Keys, Shortcuts, and Accessibility

* Provide **discoverable shortcuts** via a footer help view.
* Offer Emacs-style alternatives where it makes sense (e.g., `ctrl+n/p`).
* Use consistent navigation patterns across screens.

---

## 8) Testing Strategy

* Unit test message handling with deterministic messages.
* Snapshot/golden-test `View()` output for known terminal sizes.
* Fuzz-test parsers/formatters used by the UI.

```go
func TestUpdate_Select(t *testing.T) {
    m := newTestModel()
    _, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
    if got, want := m.cursor, 1; got != want { t.Fatalf("cursor=%d want %d", got, want) }
}
```

---

## 9) Project Structure Template

```
cmd/
  app/
    main.go
internal/
  tui/
    model.go      // root model, styles
    update.go     // Update + messages
    view.go       // View
    keys.go       // keymap/help
    components/   // submodels
  domain/        // business logic, pure Go
  io/            // adapters (API, FS, net)

Makefile         // lint, test, run targets
```

---

## 10) Scaffolding Snippets (Ask Copilot to use these)

### 10.1 Root main.go

```go
package main

import (
    "context"
    "log"
    "os"

    tea "github.com/charmbracelet/bubbletea"
)

func main() {
    if !isTTY() { // optional: detect piping
        log.Println("Non-interactive mode not implemented.")
        return
    }

    p := tea.NewProgram(NewModel(), tea.WithAltScreen())
    if _, err := p.Run(); err != nil {
        log.Fatalf("error: %v", err)
    }
}
```

### 10.2 NewModel()

```go
func NewModel() Model {
    s := NewStyles(0)
    return Model{
        list:    newList(),
        spinner: spinner.New(),
        styles:  s,
    }
}
```

### 10.3 Custom messages

```go
type (
    errMsg      error
    itemsLoaded []Item
)
```

### 10.4 Command helper

```go
func do(cmd func(context.Context) (tea.Msg, error)) tea.Cmd {
    return func() tea.Msg {
        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()
        msg, err := cmd(ctx)
        if err != nil { return errMsg(err) }
        return msg
    }
}
```

---

## 11) Dependencies to Prefer

* `github.com/charmbracelet/bubbletea`
* `github.com/charmbracelet/bubbles`
* `github.com/charmbracelet/lipgloss`
* `golang.org/x/sync/errgroup` (for non-UI workloads)
* `log/slog` (Go 1.21+) for logging

Pin versions in `go.mod`. Avoid extra UI deps unless justified.

---

## 12) Copilot Prompting Rules (Important)

* When the user writes a new TUI screen, **scaffold** `(Model, Init, Update, View)` with:

  * Window size handling
  * Keymap/help wiring
  * Styles struct and `NewStyles(width)`
  * Commands for all IO
* Prefer **typed messages** and return **`tea.Cmd`**; do not perform blocking work in `Update`.
* Always update child bubble components via `child.Update(msg)` and collect cmds with `tea.Batch`.
* Generate **tests** for key message paths.
* Include **help footer** with keybindings.
* Keep `View()` pure and free of side effects.

**Bad**

* Doing HTTP/FS work directly in `Update`.
* Printing to stdout from `Update`/`View`.
* Storing `context.Context` in the model.
* Creating goroutines that outlive the program.

**Good**

* Commands that return typed messages.
* Centralized keymap + help.
* Single source of truth for styles.
* Small submodels and composition.

---

## 13) Security & Reliability

* Validate all external inputs; sanitize strings rendered into the terminal.
* Respect user locale and UTF-8; avoid slicing strings by bytes for widths (use `lipgloss.Width`).
* Handle small terminal sizes (min-width fallbacks).
* Ensure graceful shutdown; propagate quit via `tea.Quit` and cancel pending work.

---

## 14) Makefile Targets (suggested)

```
.PHONY: run test lint fmt tidy
run:; go run ./cmd/app
fmt:; go fmt ./...
lint:; golangci-lint run
 test:; go test ./...
tidy:; go mod tidy
```

---

## 15) Example Key Handling Pattern

```go
case tea.KeyMsg:
    switch {
    case key.Matches(msg, keys.Quit):
        return m, tea.Quit
    case key.Matches(msg, keys.Up):
        if m.cursor > 0 { m.cursor-- }
    case key.Matches(msg, keys.Down):
        if m.cursor < len(m.items)-1 { m.cursor++ }
    }
```

---

## 16) Documentation & Comments

* Exported types/functions must have a sentence GoDoc.
* At the top of each file, include a short comment describing its responsibility.
* For non-obvious state transitions, include a brief state diagram in comments.

---

## 17) Acceptance Criteria for Generated Code

* Builds with `go build ./...`
* Passes `go vet` and `golangci-lint` (if configured)
* Has at least one table-driven test per major update path
* Handles window resize and quit
* No side effects in `View()`
* Commands wrap errors and return `errMsg`

---

*End of instructions.*
