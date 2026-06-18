package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/agenticraptor/ghostwriter/internal/intent"
	"github.com/agenticraptor/ghostwriter/internal/review"
	"github.com/agenticraptor/ghostwriter/internal/risk"
)

type styles struct {
	ghost   lipgloss.Style
	dim     lipgloss.Style
	repo    lipgloss.Style
	num     lipgloss.Style
	title   lipgloss.Style
	summary lipgloss.Style
	why     lipgloss.Style
	files   lipgloss.Style
	sep     lipgloss.Style
	warn    lipgloss.Style
	cat     map[intent.Category]lipgloss.Style
	sev     map[risk.Severity]lipgloss.Style
	width   int
}

func newStyles(w io.Writer, color bool, width int) styles {
	r := lipgloss.NewRenderer(w)
	if color {
		r.SetColorProfile(termenv.ANSI256)
	} else {
		r.SetColorProfile(termenv.Ascii)
	}
	c := func(code string) lipgloss.Color { return lipgloss.Color(code) }
	badge := func(code string) lipgloss.Style {
		return r.NewStyle().Bold(true).Foreground(c("0")).Background(c(code)).
			Padding(0, 1).Width(10).Align(lipgloss.Center)
	}
	chip := func(code string) lipgloss.Style {
		return r.NewStyle().Foreground(c(code)).Bold(true)
	}
	return styles{
		width: width,
		ghost: r.NewStyle().Bold(true).Foreground(c("213")),
		dim:   r.NewStyle().Foreground(c("245")),
		repo:  r.NewStyle().Bold(true).Foreground(c("231")),
		num:   r.NewStyle().Bold(true).Foreground(c("245")),
		title: r.NewStyle().Bold(true).Foreground(c("231")),
		summary: r.NewStyle().Foreground(c("252")).
			MarginLeft(5).Width(maxInt(20, width-5)),
		why: r.NewStyle().Italic(true).Foreground(c("245")).
			MarginLeft(5).Width(maxInt(20, width-5)),
		files: r.NewStyle().Foreground(c("244")).MarginLeft(5),
		sep:   r.NewStyle().Foreground(c("238")),
		warn:  r.NewStyle().Foreground(c("214")),
		cat: map[intent.Category]lipgloss.Style{
			intent.Feature:  badge("42"),
			intent.Fix:      badge("203"),
			intent.Refactor: badge("75"),
			intent.Test:     badge("170"),
			intent.Docs:     badge("80"),
			intent.Deps:     badge("214"),
			intent.Config:   badge("109"),
			intent.Chore:    badge("245"),
			intent.Other:    badge("245"),
		},
		sev: map[risk.Severity]lipgloss.Style{
			risk.High: chip("203"),
			risk.Warn: chip("214"),
			risk.Info: chip("75"),
		},
	}
}

func renderTerm(w io.Writer, r *review.Review, o Options) error {
	st := newStyles(w, o.Color, o.Width)
	files, add, del := r.Totals()

	fmt.Fprintf(w, "%s  %s %s\n",
		st.ghost.Render("👻 ghostwriter"),
		st.dim.Render("review of"),
		st.repo.Render(repoName(r.Repo)))

	if r.Empty() {
		fmt.Fprintln(w, st.dim.Render("\nNo pending changes to review. You're all caught up. ✨"))
		return nil
	}

	fmt.Fprintln(w, st.dim.Render(fmt.Sprintf("%d intents · %d files · %s · %s",
		len(r.Intents), files, diffStat(add, del), provider(r))))
	if r.Warning != "" {
		fmt.Fprintln(w, st.warn.Render("ℹ "+r.Warning))
	}
	fmt.Fprintln(w)

	for i, in := range r.Intents {
		badge, ok := st.cat[in.Category]
		if !ok {
			badge = st.cat[intent.Other]
		}
		fmt.Fprintf(w, "%s %s  %s\n",
			st.num.Render(fmt.Sprintf("%2d", i+1)),
			badge.Render(categoryLabel(in.Category)),
			st.title.Render(in.Title))
		if in.Summary != "" {
			fmt.Fprintln(w, st.summary.Render(in.Summary))
		}
		if in.Why != "" {
			fmt.Fprintln(w, st.why.Render("why: "+in.Why))
		}
		meta := strings.Join(in.Files, " · ")
		if meta == "" {
			meta = "(no file changes)"
		}
		fmt.Fprintln(w, st.files.Render(meta+"  "+diffStat(in.Additions, in.Deletions)))
		if len(in.Risks) > 0 {
			fmt.Fprintln(w, "     "+riskChips(st, in.Risks))
		}
		fmt.Fprintln(w)
	}

	if len(r.Deps) > 0 {
		fmt.Fprintln(w, st.dim.Render("dependency changes:"))
		for _, c := range r.Deps {
			sign := st.sev[risk.Info].Render("＋")
			if !c.Added {
				sign = st.sev[risk.High].Render("－")
			}
			fmt.Fprintf(w, "  %s %s %s %s\n", sign,
				st.repo.Render(c.Name), st.dim.Render(c.Version), st.dim.Render("("+c.Ecosystem+")"))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, st.sep.Render(strings.Repeat("─", minInt(o.Width, 62))))
	fmt.Fprintln(w, st.dim.Render(fmt.Sprintf(
		"%d intents · %d files · %s · %s", len(r.Intents), files, diffStat(add, del), provider(r))))
	return nil
}

func riskChips(st styles, flags []risk.Flag) string {
	var parts []string
	for _, fl := range flags {
		style := st.sev[fl.Severity]
		glyph := "•"
		switch fl.Severity {
		case risk.High:
			glyph = "⚠"
		case risk.Warn:
			glyph = "▲"
		case risk.Info:
			glyph = "ℹ"
		}
		parts = append(parts, style.Render(glyph+" "+fl.Label))
	}
	return strings.Join(parts, st.dim.Render("  "))
}

func repoName(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" || p == "." {
		return "this repo"
	}
	if i := strings.LastIndexByte(p, '/'); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return p
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
