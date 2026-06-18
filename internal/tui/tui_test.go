package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
	"github.com/agenticraptor/ghostwriter/internal/intent"
	"github.com/agenticraptor/ghostwriter/internal/review"
)

func threeIntentReview() *review.Review {
	return &review.Review{
		Intents: []intent.Intent{
			{Title: "Add retry logic", Category: intent.Feature, Hunks: []intent.HunkRef{{File: 0, Hunk: 0}}},
			{Title: "Update tests", Category: intent.Test},
			{Title: "Bump SDK", Category: intent.Deps},
		},
		Diff: gitdiff.Diff{Files: []gitdiff.File{{
			NewPath: "pay.go",
			Hunks: []gitdiff.Hunk{{Lines: []gitdiff.Line{
				{Kind: gitdiff.Insert, Text: "retry()"},
			}}},
		}}},
	}
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func send(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	nm, _ := m.Update(msg)
	return nm.(model)
}

func TestAcceptRejectAdvances(t *testing.T) {
	m := newModel(threeIntentReview())

	m = send(t, m, runes("a"))
	if m.decisions[0] != Accept {
		t.Fatalf("intent 0 = %v, want Accept", m.decisions[0])
	}
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (accept should advance)", m.cursor)
	}

	m = send(t, m, runes("r"))
	if m.decisions[1] != Reject {
		t.Fatalf("intent 1 = %v, want Reject", m.decisions[1])
	}

	m = send(t, m, runes(" ")) // cycle pending -> accept on intent 2
	if m.decisions[2] != Accept {
		t.Fatalf("intent 2 = %v, want Accept after one cycle", m.decisions[2])
	}

	a, r, p := m.Tally()
	if a != 2 || r != 1 || p != 0 {
		t.Fatalf("tally a/r/p = %d/%d/%d, want 2/1/0", a, r, p)
	}
}

func TestBulkRejectAndConfirm(t *testing.T) {
	m := newModel(threeIntentReview())
	m = send(t, m, runes("R"))
	for i, d := range m.decisions {
		if d != Reject {
			t.Fatalf("intent %d = %v, want Reject", i, d)
		}
	}
	nm, cmd := m.Update(runes("q"))
	m = nm.(model)
	if !m.confirmed {
		t.Fatal("q should set confirmed=true")
	}
	if cmd == nil {
		t.Fatal("q should issue a quit command")
	}
}

func TestEscCancels(t *testing.T) {
	m := newModel(threeIntentReview())
	m = send(t, m, runes("A"))
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(model)
	if m.confirmed {
		t.Fatal("esc must not confirm")
	}
	if cmd == nil {
		t.Fatal("esc should still quit")
	}
}

func TestViewRendersWithoutPanic(t *testing.T) {
	m := newModel(threeIntentReview())
	m.width, m.height = 100, 30
	if out := m.View(); out == "" {
		t.Fatal("expected non-empty view")
	}
	m.showHelp = true
	if out := m.View(); out == "" {
		t.Fatal("expected non-empty help view")
	}
}
