package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/agenticraptor/ghostwriter/internal/intent"
	"github.com/agenticraptor/ghostwriter/internal/risk"
)

type styles struct {
	ghost    lipgloss.Style
	dim      lipgloss.Style
	sep      lipgloss.Style
	cursor   lipgloss.Style
	selected lipgloss.Style
	title    lipgloss.Style
	summary  lipgloss.Style
	why      lipgloss.Style
	fileHdr  lipgloss.Style
	hunkHdr  lipgloss.Style
	insert   lipgloss.Style
	delete   lipgloss.Style
	context  lipgloss.Style
	accept   lipgloss.Style
	reject   lipgloss.Style

	cat map[intent.Category]lipgloss.Style
	sev map[risk.Severity]lipgloss.Style
}

func newStyles() styles {
	c := func(code string) lipgloss.Color { return lipgloss.Color(code) }
	badge := func(code string) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Foreground(c("0")).Background(c(code)).Padding(0, 1)
	}
	return styles{
		ghost:    lipgloss.NewStyle().Bold(true).Foreground(c("213")),
		dim:      lipgloss.NewStyle().Foreground(c("245")),
		sep:      lipgloss.NewStyle().Foreground(c("238")),
		cursor:   lipgloss.NewStyle().Foreground(c("213")),
		selected: lipgloss.NewStyle().Bold(true).Foreground(c("231")),
		title:    lipgloss.NewStyle().Bold(true).Foreground(c("231")),
		summary:  lipgloss.NewStyle().Foreground(c("252")),
		why:      lipgloss.NewStyle().Italic(true).Foreground(c("245")),
		fileHdr:  lipgloss.NewStyle().Bold(true).Foreground(c("75")),
		hunkHdr:  lipgloss.NewStyle().Foreground(c("109")),
		insert:   lipgloss.NewStyle().Foreground(c("42")),
		delete:   lipgloss.NewStyle().Foreground(c("203")),
		context:  lipgloss.NewStyle().Foreground(c("244")),
		accept:   lipgloss.NewStyle().Bold(true).Foreground(c("42")),
		reject:   lipgloss.NewStyle().Bold(true).Foreground(c("203")),
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
			risk.High: lipgloss.NewStyle().Bold(true).Foreground(c("203")),
			risk.Warn: lipgloss.NewStyle().Bold(true).Foreground(c("214")),
			risk.Info: lipgloss.NewStyle().Foreground(c("75")),
		},
	}
}

func (s styles) badge(cat intent.Category) lipgloss.Style {
	if st, ok := s.cat[cat]; ok {
		return st
	}
	return s.cat[intent.Other]
}

func (s styles) severity(sev risk.Severity) lipgloss.Style {
	if st, ok := s.sev[sev]; ok {
		return st
	}
	return s.dim
}
