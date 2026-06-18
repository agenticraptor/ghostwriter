// Package intent groups the raw hunks of a diff into human-meaningful
// "intents" — the handful of things an agent was actually trying to do — so a
// reviewer reads a short story instead of a wall of diff.
//
// There are two grouping strategies: a deterministic, offline fallback (one
// intent per file) and an LLM-powered narrator that clusters related hunks
// across files and explains them in plain English. Either way, every line
// count and risk flag is recomputed from the real diff, so the numbers are
// always trustworthy even if the model is not.
package intent

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
	"github.com/agenticraptor/ghostwriter/internal/risk"
)

// Category is a coarse classification of an intent.
type Category string

// Categories.
const (
	Feature  Category = "feature"
	Fix      Category = "fix"
	Refactor Category = "refactor"
	Test     Category = "test"
	Docs     Category = "docs"
	Deps     Category = "deps"
	Config   Category = "config"
	Chore    Category = "chore"
	Other    Category = "other"
)

// Valid reports whether c is a known category, normalizing as needed.
func (c Category) normalize() Category {
	switch Category(strings.ToLower(strings.TrimSpace(string(c)))) {
	case Feature:
		return Feature
	case Fix:
		return Fix
	case Refactor:
		return Refactor
	case Test:
		return Test
	case Docs:
		return Docs
	case Deps:
		return Deps
	case Config:
		return Config
	case Chore:
		return Chore
	default:
		return Other
	}
}

// HunkRef points at one hunk within a Diff.
type HunkRef struct {
	File int
	Hunk int
}

// Intent is a group of related hunks with a plain-English explanation.
type Intent struct {
	Title     string
	Summary   string
	Why       string
	Category  Category
	Hunks     []HunkRef
	Risks     []risk.Flag
	Files     []string
	Additions int
	Deletions int
}

// HighestRisk returns the maximum severity among the intent's risk flags.
func (in Intent) HighestRisk() risk.Severity { return risk.Max(in.Risks) }

// Deterministic groups the diff with no model: one intent per file. It always
// works and is the fallback whenever the AI path is unavailable or fails.
func Deterministic(d gitdiff.Diff) []Intent {
	intents := make([]Intent, 0, len(d.Files))
	for fi, f := range d.Files {
		var refs []HunkRef
		for hi := range f.Hunks {
			refs = append(refs, HunkRef{File: fi, Hunk: hi})
		}
		if len(refs) == 0 {
			refs = []HunkRef{{File: fi, Hunk: -1}} // binary / pure-rename file with no hunks
		}
		intents = append(intents, Intent{
			Title:    titleForFile(f),
			Summary:  summaryForFile(f),
			Category: categoryForPath(f.Path()),
			Hunks:    refs,
		})
	}
	Annotate(intents, d)
	return intents
}

// Annotate recomputes Files, Additions, Deletions, and Risks for each intent
// directly from the diff, and drops references that point nowhere.
func Annotate(intents []Intent, d gitdiff.Diff) {
	for i := range intents {
		in := &intents[i]
		fileSet := map[int]bool{}
		var cleanRefs []HunkRef
		add, del := 0, 0
		for _, ref := range in.Hunks {
			if ref.File < 0 || ref.File >= len(d.Files) {
				continue
			}
			f := d.Files[ref.File]
			fileSet[ref.File] = true
			cleanRefs = append(cleanRefs, ref)
			if ref.Hunk >= 0 && ref.Hunk < len(f.Hunks) {
				add += f.Hunks[ref.Hunk].Additions
				del += f.Hunks[ref.Hunk].Deletions
			}
		}
		in.Hunks = cleanRefs
		in.Additions, in.Deletions = add, del

		fileIdx := make([]int, 0, len(fileSet))
		for fi := range fileSet {
			fileIdx = append(fileIdx, fi)
		}
		sort.Ints(fileIdx)
		in.Files = in.Files[:0]
		var flags []risk.Flag
		for _, fi := range fileIdx {
			in.Files = append(in.Files, d.Files[fi].Path())
			flags = append(flags, risk.Analyze(d.Files[fi])...)
		}
		in.Risks = mergeFlags(flags)
		if in.Category == "" {
			in.Category = Other
		}
	}
}

// FileHunks converts an intent's references into the map shape gitdiff.Revert
// expects: file index -> hunk indices (empty slice means the whole file).
func FileHunks(in Intent) map[int][]int {
	out := map[int][]int{}
	for _, ref := range in.Hunks {
		if ref.Hunk < 0 {
			if _, ok := out[ref.File]; !ok {
				out[ref.File] = nil
			}
			continue
		}
		out[ref.File] = append(out[ref.File], ref.Hunk)
	}
	return out
}

func titleForFile(f gitdiff.File) string {
	name := filepath.Base(f.Path())
	switch f.Status {
	case gitdiff.Added:
		return "Add " + name
	case gitdiff.Deleted:
		return "Delete " + name
	case gitdiff.Renamed:
		return fmt.Sprintf("Rename %s → %s", filepath.Base(f.OldPath), filepath.Base(f.NewPath))
	default:
		return "Update " + name
	}
}

func summaryForFile(f gitdiff.File) string {
	switch {
	case f.Binary:
		return fmt.Sprintf("Binary file `%s` changed.", f.Path())
	case f.Status == gitdiff.Added:
		return fmt.Sprintf("New file `%s` with %d line(s).", f.Path(), f.Additions)
	case f.Status == gitdiff.Deleted:
		return fmt.Sprintf("Removed `%s` (%d line(s)).", f.Path(), f.Deletions)
	case f.Status == gitdiff.Renamed:
		return fmt.Sprintf("Moved `%s` to `%s`.", f.OldPath, f.NewPath)
	default:
		return fmt.Sprintf("Changed `%s` (+%d/−%d).", f.Path(), f.Additions, f.Deletions)
	}
}

func categoryForPath(path string) Category {
	lower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))
	switch {
	case isTestPath(lower):
		return Test
	case base == "package.json" || base == "go.mod" || base == "requirements.txt" ||
		base == "cargo.toml" || base == "pyproject.toml" || base == "go.sum" ||
		strings.HasSuffix(base, ".lock"):
		return Deps
	case strings.HasSuffix(base, ".md") || strings.Contains(lower, "/docs/") || base == "readme":
		return Docs
	case strings.Contains(lower, ".github/") || base == "dockerfile" || base == "makefile" ||
		strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml") ||
		strings.HasSuffix(base, ".toml") || strings.HasSuffix(base, ".ini"):
		return Config
	default:
		return Other
	}
}

func isTestPath(lower string) bool {
	return strings.Contains(lower, "_test.") || strings.Contains(lower, ".test.") ||
		strings.Contains(lower, ".spec.") || strings.Contains(lower, "/tests/") ||
		strings.Contains(lower, "/test/")
}

func mergeFlags(flags []risk.Flag) []risk.Flag {
	seen := map[string]bool{}
	var out []risk.Flag
	for _, f := range flags {
		if seen[f.Label] {
			continue
		}
		seen[f.Label] = true
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Severity > out[j].Severity })
	return out
}
