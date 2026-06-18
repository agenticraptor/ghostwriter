// Package render turns a Review into output: a styled terminal report (the
// default), Markdown, JSON, or uncolored plain text.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/agenticraptor/ghostwriter/internal/intent"
	"github.com/agenticraptor/ghostwriter/internal/review"
	"github.com/agenticraptor/ghostwriter/internal/risk"
)

// Format selects the output representation.
type Format int

// Output formats.
const (
	Term Format = iota
	Plain
	Markdown
	JSON
)

// ParseFormat maps a string flag to a Format.
func ParseFormat(s string) (Format, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "term", "terminal", "tty":
		return Term, true
	case "plain", "text", "txt":
		return Plain, true
	case "markdown", "md":
		return Markdown, true
	case "json":
		return JSON, true
	default:
		return Term, false
	}
}

// Options controls rendering.
type Options struct {
	Format Format
	Color  bool
	Width  int
}

// Render writes the review to w in the requested format.
func Render(w io.Writer, r *review.Review, o Options) error {
	if o.Width <= 0 {
		o.Width = 80
	}
	switch o.Format {
	case JSON:
		return renderJSON(w, r)
	case Markdown:
		return renderMarkdown(w, r)
	default:
		return renderTerm(w, r, o)
	}
}

func provider(r *review.Review) string {
	if r.UsedAI {
		if r.Model != "" {
			return r.Provider + " " + r.Model
		}
		return r.Provider
	}
	return "local heuristics"
}

func diffStat(add, del int) string {
	return fmt.Sprintf("+%d/−%d", add, del)
}

// ----- JSON -----

type jsonReview struct {
	Repo       string       `json:"repo"`
	Generated  string       `json:"generated"`
	Provider   string       `json:"provider"`
	Model      string       `json:"model,omitempty"`
	UsedAI     bool         `json:"used_ai"`
	Warning    string       `json:"warning,omitempty"`
	Files      int          `json:"files"`
	Additions  int          `json:"additions"`
	Deletions  int          `json:"deletions"`
	Intents    []jsonIntent `json:"intents"`
	Dependency []jsonDep    `json:"dependency_changes,omitempty"`
}

type jsonIntent struct {
	Title     string     `json:"title"`
	Summary   string     `json:"summary"`
	Why       string     `json:"why,omitempty"`
	Category  string     `json:"category"`
	Files     []string   `json:"files"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Risks     []jsonRisk `json:"risks,omitempty"`
}

type jsonRisk struct {
	Severity string `json:"severity"`
	Label    string `json:"label"`
	Reason   string `json:"reason"`
}

type jsonDep struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Added     bool   `json:"added"`
}

func renderJSON(w io.Writer, r *review.Review) error {
	files, add, del := r.Totals()
	out := jsonReview{
		Repo:      r.Repo,
		Generated: r.Generated.Format("2006-01-02T15:04:05Z07:00"),
		Provider:  r.Provider,
		Model:     r.Model,
		UsedAI:    r.UsedAI,
		Warning:   r.Warning,
		Files:     files,
		Additions: add,
		Deletions: del,
	}
	for _, in := range r.Intents {
		ji := jsonIntent{
			Title:     in.Title,
			Summary:   in.Summary,
			Why:       in.Why,
			Category:  string(in.Category),
			Files:     in.Files,
			Additions: in.Additions,
			Deletions: in.Deletions,
		}
		for _, fl := range in.Risks {
			ji.Risks = append(ji.Risks, jsonRisk{fl.Severity.String(), fl.Label, fl.Reason})
		}
		out.Intents = append(out.Intents, ji)
	}
	for _, c := range r.Deps {
		out.Dependency = append(out.Dependency, jsonDep{c.Ecosystem, c.Name, c.Version, c.Added})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// ----- Markdown -----

func renderMarkdown(w io.Writer, r *review.Review) error {
	files, add, del := r.Totals()
	fmt.Fprintf(w, "# ghostwriter review — %s\n\n", r.Repo)
	if r.Empty() {
		fmt.Fprintln(w, "No pending changes to review. ✨")
		return nil
	}
	fmt.Fprintf(w, "_%d intent(s) · %d file(s) · %s · %s_\n\n", len(r.Intents), files, diffStat(add, del), provider(r))
	if r.Warning != "" {
		fmt.Fprintf(w, "> ℹ️ %s\n\n", r.Warning)
	}
	for i, in := range r.Intents {
		fmt.Fprintf(w, "## %d. %s `%s`\n\n", i+1, in.Title, in.Category)
		if in.Summary != "" {
			fmt.Fprintf(w, "%s\n\n", in.Summary)
		}
		if in.Why != "" {
			fmt.Fprintf(w, "_Why:_ %s\n\n", in.Why)
		}
		if len(in.Files) > 0 {
			fmt.Fprintf(w, "- **Files:** %s (%s)\n", "`"+strings.Join(in.Files, "`, `")+"`", diffStat(in.Additions, in.Deletions))
		}
		for _, fl := range in.Risks {
			fmt.Fprintf(w, "- %s **%s** — %s\n", riskGlyph(fl.Severity), fl.Label, fl.Reason)
		}
		fmt.Fprintln(w)
	}
	if len(r.Deps) > 0 {
		fmt.Fprintln(w, "## Dependency changes")
		fmt.Fprintln(w)
		for _, c := range r.Deps {
			sign := "added"
			if !c.Added {
				sign = "removed"
			}
			fmt.Fprintf(w, "- `%s` %s %s (%s)\n", c.Name, c.Version, sign, c.Ecosystem)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func riskGlyph(s risk.Severity) string {
	switch s {
	case risk.High:
		return "🔴"
	case risk.Warn:
		return "🟡"
	default:
		return "🔵"
	}
}

func categoryLabel(c intent.Category) string {
	return strings.ToUpper(string(c))
}
