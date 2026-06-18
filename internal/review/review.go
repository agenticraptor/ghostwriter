// Package review orchestrates a full ghostwriter review: it collects the diff,
// detects dependency changes, and groups the hunks into narrated intents —
// using a model when one is available and falling back to deterministic,
// offline grouping otherwise. The result is a single value the renderer and the
// TUI both consume.
package review

import (
	"context"
	"errors"
	"time"

	"github.com/agenticraptor/ghostwriter/internal/deps"
	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
	"github.com/agenticraptor/ghostwriter/internal/intent"
	"github.com/agenticraptor/ghostwriter/internal/llm"
)

// Review is the complete result of analyzing a repository's pending changes.
type Review struct {
	Repo      string
	Root      string
	Generated time.Time
	Provider  string // "anthropic" | "openai" | "ollama" | "local"
	Model     string
	UsedAI    bool
	Warning   string // why the AI path was skipped, if it was
	Diff      gitdiff.Diff
	Intents   []intent.Intent
	Deps      []deps.Change
}

// Empty reports whether there were no changes to review.
func (r *Review) Empty() bool { return r == nil || r.Diff.Empty() }

// Totals returns aggregate counts for the review.
func (r *Review) Totals() (files, additions, deletions int) {
	return len(r.Diff.Files), r.Diff.Additions(), r.Diff.Deletions()
}

// Options controls how a review is built.
type Options struct {
	Repo             string
	Against          string
	Staged           bool
	ExcludeUntracked bool

	NoAI     bool
	Provider string
	Model    string
	BaseURL  string
	MaxBytes int
}

// Build collects the diff and produces a fully annotated Review.
func Build(ctx context.Context, opts Options) (*Review, error) {
	root, err := gitdiff.Root(ctx, opts.Repo)
	if err != nil {
		return nil, err
	}
	diff, err := gitdiff.Collect(ctx, gitdiff.Options{
		Repo:             opts.Repo,
		Against:          opts.Against,
		Staged:           opts.Staged,
		ExcludeUntracked: opts.ExcludeUntracked,
		MaxFileBytes:     0,
	})
	if err != nil {
		return nil, err
	}

	r := &Review{
		Repo:      opts.Repo,
		Root:      root,
		Generated: time.Now(),
		Provider:  "local",
		Diff:      diff,
		Deps:      deps.Analyze(diff),
	}
	if diff.Empty() {
		return r, nil
	}

	if opts.NoAI {
		r.Intents = intent.Deterministic(diff)
		return r, nil
	}

	client, err := newClient(opts)
	if err != nil {
		r.Warning = aiSkipReason(err)
		r.Intents = intent.Deterministic(diff)
		return r, nil
	}

	intents, err := intent.Narrate(ctx, client, diff, opts.MaxBytes)
	if err != nil {
		r.Warning = "AI narration failed (" + err.Error() + "); showing heuristic grouping."
		r.Intents = intent.Deterministic(diff)
		return r, nil
	}

	r.Intents = intents
	r.Provider = client.Name()
	r.Model = client.Model()
	r.UsedAI = true
	return r, nil
}

func newClient(opts Options) (llm.Client, error) {
	if opts.Provider != "" {
		return llm.New(opts.Provider, opts.Model, opts.BaseURL)
	}
	return llm.Detect()
}

func aiSkipReason(err error) string {
	switch {
	case errors.Is(err, llm.ErrNoCredentials):
		return "No API key found; showing heuristic grouping. Set ANTHROPIC_API_KEY or OPENAI_API_KEY (or run Ollama) for AI narration."
	case errors.Is(err, llm.ErrUnknownProvider):
		return "Unknown provider; showing heuristic grouping."
	default:
		return "AI unavailable (" + err.Error() + "); showing heuristic grouping."
	}
}
