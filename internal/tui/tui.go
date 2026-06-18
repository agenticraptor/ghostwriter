// Package tui implements ghostwriter's interactive review: a Bubble Tea program
// that shows each intent as a card with its underlying diff and lets you accept
// or reject it with a single keystroke. It only records decisions; applying the
// rejections back to the working tree is the caller's job.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
	"github.com/agenticraptor/ghostwriter/internal/intent"
	"github.com/agenticraptor/ghostwriter/internal/review"
	"github.com/agenticraptor/ghostwriter/internal/risk"
)

// Decision is the reviewer's verdict on a single intent.
type Decision int

// Decisions.
const (
	Pending Decision = iota
	Accept
	Reject
)

// Result is returned when the program exits.
type Result struct {
	Decisions []Decision
	Confirmed bool // true if the reviewer chose to finish (not abort)
}

// Run launches the interactive review and blocks until the reviewer finishes.
func Run(r *review.Review) (Result, error) {
	m := newModel(r)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return Result{}, err
	}
	fm := final.(model)
	return Result{Decisions: fm.decisions, Confirmed: fm.confirmed}, nil
}

type model struct {
	rev       *review.Review
	decisions []Decision
	cursor    int
	scroll    int
	width     int
	height    int
	showHelp  bool
	confirmed bool
	st        styles
}

func newModel(r *review.Review) model {
	return model{
		rev:       r,
		decisions: make([]Decision, len(r.Intents)),
		width:     80,
		height:    24,
		st:        newStyles(),
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.rev.Intents)
	switch msg.String() {
	case "ctrl+c", "esc":
		m.confirmed = false
		return m, tea.Quit
	case "q", "enter":
		m.confirmed = true
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.scroll = 0
		}
	case "down", "j":
		if m.cursor < n-1 {
			m.cursor++
			m.scroll = 0
		}
	case "a":
		m.setDecision(Accept)
		m.advance()
	case "r":
		m.setDecision(Reject)
		m.advance()
	case "u", "backspace":
		m.setDecision(Pending)
	case " ", "space":
		m.cycle()
	case "A":
		for i := range m.decisions {
			m.decisions[i] = Accept
		}
	case "R":
		for i := range m.decisions {
			m.decisions[i] = Reject
		}
	case "J", "pgdown":
		m.scroll += 5
	case "K", "pgup":
		m.scroll -= 5
		if m.scroll < 0 {
			m.scroll = 0
		}
	}
	return m, nil
}

func (m *model) setDecision(d Decision) {
	if m.cursor >= 0 && m.cursor < len(m.decisions) {
		m.decisions[m.cursor] = d
	}
}

func (m *model) cycle() {
	if m.cursor < 0 || m.cursor >= len(m.decisions) {
		return
	}
	m.decisions[m.cursor] = (m.decisions[m.cursor] + 1) % 3
}

func (m *model) advance() {
	if m.cursor < len(m.rev.Intents)-1 {
		m.cursor++
		m.scroll = 0
	}
}

// Tally returns the number of accepted, rejected, and pending intents.
func (m model) Tally() (accept, reject, pending int) {
	for _, d := range m.decisions {
		switch d {
		case Accept:
			accept++
		case Reject:
			reject++
		default:
			pending++
		}
	}
	return accept, reject, pending
}

func (m model) View() string {
	if len(m.rev.Intents) == 0 {
		return m.st.dim.Render("\n  No pending changes to review. Press q to exit.\n")
	}
	if m.showHelp {
		return m.helpView()
	}

	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteString("\n\n")
	b.WriteString(m.listView())
	b.WriteString("\n")
	b.WriteString(m.st.sep.Render(strings.Repeat("─", clampWidth(m.width))))
	b.WriteString("\n")
	b.WriteString(m.detailView())
	b.WriteString("\n")
	b.WriteString(m.footerView())
	return b.String()
}

func (m model) headerView() string {
	a, r, p := m.Tally()
	left := m.st.ghost.Render("👻 ghostwriter")
	status := fmt.Sprintf("%s  %s  %s",
		m.st.accept.Render(fmt.Sprintf("✓ %d accept", a)),
		m.st.reject.Render(fmt.Sprintf("✗ %d reject", r)),
		m.st.dim.Render(fmt.Sprintf("· %d pending", p)))
	return left + "   " + status
}

func (m model) listView() string {
	var b strings.Builder
	for i, in := range m.rev.Intents {
		marker := m.decisionMarker(m.decisions[i])
		badge := m.st.badge(in.Category).Render(strings.ToUpper(string(in.Category)))
		title := in.Title
		line := fmt.Sprintf("%s %s %s  %s", marker, m.st.dim.Render(fmt.Sprintf("%2d", i+1)), badge, title)
		risky := ""
		if in.HighestRisk() == risk.High {
			risky = " " + m.st.reject.Render("⚠")
		}
		if i == m.cursor {
			b.WriteString(m.st.cursor.Render("▌ ") + m.st.selected.Render(line) + risky)
		} else {
			b.WriteString("  " + line + risky)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m model) detailView() string {
	in := m.rev.Intents[m.cursor]
	lines := m.detailLines(in)

	visible := m.height - len(m.rev.Intents) - 9
	if visible < 4 {
		visible = 4
	}
	if m.scroll > len(lines)-1 {
		m.scroll = maxInt(0, len(lines)-1)
	}
	end := m.scroll + visible
	if end > len(lines) {
		end = len(lines)
	}
	window := lines[m.scroll:end]
	more := ""
	if end < len(lines) {
		more = m.st.dim.Render(fmt.Sprintf("  … %d more lines (J/K to scroll)", len(lines)-end))
	}
	return strings.Join(window, "\n") + "\n" + more
}

func (m model) detailLines(in intent.Intent) []string {
	var lines []string
	add := func(s string) { lines = append(lines, s) }

	add(m.st.title.Render(in.Title) + "  " + m.st.dim.Render(diffStat(in.Additions, in.Deletions)))
	if in.Summary != "" {
		add(m.st.summary.Render(in.Summary))
	}
	if in.Why != "" {
		add(m.st.why.Render("why: " + in.Why))
	}
	for _, fl := range in.Risks {
		add("  " + m.st.severity(fl.Severity).Render("⚠ "+fl.Label) + m.st.dim.Render(" — "+fl.Reason))
	}
	add("")
	for _, ref := range in.Hunks {
		if ref.File < 0 || ref.File >= len(m.rev.Diff.Files) {
			continue
		}
		f := m.rev.Diff.Files[ref.File]
		add(m.st.fileHdr.Render("▸ " + f.Path()))
		if ref.Hunk < 0 || ref.Hunk >= len(f.Hunks) {
			if f.Binary {
				add(m.st.dim.Render("    (binary change)"))
			}
			continue
		}
		h := f.Hunks[ref.Hunk]
		if h.Section != "" {
			add(m.st.hunkHdr.Render("  @@ " + h.Section))
		}
		for _, ln := range h.Lines {
			add(m.renderDiffLine(ln))
		}
	}
	return lines
}

func (m model) renderDiffLine(ln gitdiff.Line) string {
	text := clip(ln.Text, clampWidth(m.width)-2)
	switch ln.Kind {
	case gitdiff.Insert:
		return m.st.insert.Render("  + " + text)
	case gitdiff.Delete:
		return m.st.delete.Render("  - " + text)
	default:
		return m.st.context.Render("    " + text)
	}
}

func (m model) footerView() string {
	keys := []string{"↑/↓ move", "a accept", "r reject", "space cycle", "A/R all", "J/K scroll", "? help", "q finish", "esc cancel"}
	return m.st.dim.Render(strings.Join(keys, "  ·  "))
}

func (m model) helpView() string {
	help := `
  👻 ghostwriter — interactive review

  Navigate
    ↑ / k        previous intent
    ↓ / j        next intent
    J / K        scroll the diff down / up
    pgdn / pgup  scroll the diff a page

  Decide
    a            accept the current intent (keep the change)
    r            reject the current intent (revert the change)
    space        cycle pending → accept → reject
    u            reset the current intent to pending
    A            accept every intent
    R            reject every intent

  Finish
    q / enter    finish and apply your rejections
    esc / ctrl+c cancel without changing anything

  Press ? to return.
`
	return m.st.dim.Render(help)
}

func (m model) decisionMarker(d Decision) string {
	switch d {
	case Accept:
		return m.st.accept.Render("✓")
	case Reject:
		return m.st.reject.Render("✗")
	default:
		return m.st.dim.Render("·")
	}
}

func diffStat(add, del int) string { return fmt.Sprintf("+%d/−%d", add, del) }

func clampWidth(w int) int {
	if w <= 0 {
		return 80
	}
	if w > 100 {
		return 100
	}
	return w
}

func clip(s string, n int) string {
	if n < 4 {
		n = 4
	}
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
