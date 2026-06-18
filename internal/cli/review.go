package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/agenticraptor/ghostwriter/internal/config"
	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
	"github.com/agenticraptor/ghostwriter/internal/intent"
	"github.com/agenticraptor/ghostwriter/internal/render"
	"github.com/agenticraptor/ghostwriter/internal/review"
	"github.com/agenticraptor/ghostwriter/internal/tui"
)

type reviewOptions struct {
	repo        string
	against     string
	staged      bool
	noUntracked bool
	noAI        bool
	provider    string
	model       string
	format      string
	output      string
	print       bool
	yes         bool
	noColor     bool
	quiet       bool
	maxBytes    int
}

func runReview(ctx context.Context, o reviewOptions) error {
	cfg, cfgErr := config.Load()
	if cfgErr != nil && !o.quiet {
		fmt.Fprintf(os.Stderr, "warning: %v (using defaults)\n", cfgErr)
	}

	format, ok := render.ParseFormat(o.format)
	if !ok {
		return fmt.Errorf("unknown format %q (want term, plain, markdown, or json)", o.format)
	}

	opts := review.Options{
		Repo:             o.repo,
		Against:          firstNonEmpty(o.against, cfg.Review.Against),
		Staged:           o.staged,
		ExcludeUntracked: o.noUntracked || !cfg.Review.IncludeUntracked,
		NoAI:             o.noAI || !cfg.AI.Enabled,
		Provider:         firstNonEmpty(o.provider, cfg.AI.Provider),
		Model:            firstNonEmpty(o.model, cfg.AI.Model),
		MaxBytes:         firstPositive(o.maxBytes, cfg.AI.MaxDiffBytes),
	}

	stdoutTTY := isTTY(os.Stdout)
	interactive := !o.print && o.output == "" && o.format == "" && stdoutTTY && isTTY(os.Stdin)

	if interactive && !o.quiet {
		fmt.Fprint(os.Stderr, "👻 reading your changes…\r")
	}
	rev, err := review.Build(ctx, opts)
	if interactive && !o.quiet {
		fmt.Fprint(os.Stderr, strings.Repeat(" ", 28)+"\r")
	}
	if err != nil {
		return err
	}

	if rev.Empty() || !interactive {
		return writeReview(rev, format, o, stdoutTTY)
	}
	return interactiveReview(ctx, rev, o)
}

// writeReview renders the review without modifying any files.
func writeReview(rev *review.Review, format render.Format, o reviewOptions, stdoutTTY bool) error {
	var w io.Writer = os.Stdout
	var closer io.Closer
	if o.output != "" {
		f, err := os.Create(o.output) //nolint:gosec // user-specified output path
		if err != nil {
			return err
		}
		w, closer = f, f
	}

	color := format == render.Term && o.output == "" && !o.noColor && stdoutTTY
	err := render.Render(w, rev, render.Options{Format: format, Color: color, Width: termWidth()})
	if closer != nil {
		_ = closer.Close()
	}
	if err == nil && o.output != "" && !o.quiet {
		fmt.Fprintf(os.Stderr, "Review written to %s\n", o.output)
	}
	return err
}

// interactiveReview runs the TUI and applies the reviewer's rejections.
func interactiveReview(ctx context.Context, rev *review.Review, o reviewOptions) error {
	res, err := tui.Run(rev)
	if err != nil {
		return err
	}
	if !res.Confirmed {
		if !o.quiet {
			fmt.Fprintln(os.Stderr, "Review canceled — nothing was changed.")
		}
		return nil
	}

	merged := map[int][]int{}
	rejected := 0
	for i, in := range rev.Intents {
		if i < len(res.Decisions) && res.Decisions[i] == tui.Reject {
			rejected++
			mergeFileHunks(merged, intent.FileHunks(in))
		}
	}

	if rejected == 0 {
		fmt.Printf("✓ Accepted all %d intent(s). Your working tree is unchanged.\n", len(rev.Intents))
		return nil
	}

	if !o.yes {
		fmt.Printf("Revert %d intent(s) across %d file(s)? This rewrites your working tree. [y/N] ", rejected, len(merged))
		if !confirm() {
			fmt.Println("Canceled — nothing was changed.")
			return nil
		}
	}

	color := !o.noColor
	green := func(s string) string { return colorize(s, "32", color) }
	red := func(s string) string { return colorize(s, "31", color) }

	// Save a restorable backup of what we're about to revert, so the reviewer
	// can never permanently lose accepted-then-rejected work.
	backup, _ := gitdiff.WriteRejectionBackup(ctx, rev.Root, rev.Diff, merged)

	results := gitdiff.Revert(ctx, rev.Root, rev.Diff, merged)
	okCount, failCount := 0, 0
	for _, r := range results {
		if r.OK {
			okCount++
			verb := "reverted"
			if r.Deleted {
				verb = "removed new file"
			}
			fmt.Printf("  %s %s %s\n", green("✓"), verb, r.Path)
			continue
		}
		failCount++
		fmt.Fprintf(os.Stderr, "  %s could not revert %s: %v\n", red("✗"), r.Path, r.Err)
	}

	fmt.Printf("\nReverted %d file(s); kept everything you accepted.\n", okCount)
	if backup != "" {
		fmt.Printf("Backed up the reverted changes — restore them any time with:\n  git apply %q\n", backup)
	}
	if failCount > 0 {
		fmt.Fprintf(os.Stderr, "%d file(s) could not be reverted automatically — review them by hand (e.g. `git diff`).\n", failCount)
	}
	return nil
}

// colorize wraps s in an ANSI SGR code when enabled.
func colorize(s, code string, enabled bool) string {
	if !enabled {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

// mergeFileHunks unions src into dst. A nil hunk list means "the whole file"
// and always wins over a specific selection.
func mergeFileHunks(dst map[int][]int, src map[int][]int) {
	for fi, hunks := range src {
		existing, ok := dst[fi]
		if hunks == nil || (ok && existing == nil) {
			dst[fi] = nil
			continue
		}
		set := map[int]bool{}
		for _, h := range existing {
			set[h] = true
		}
		for _, h := range hunks {
			set[h] = true
		}
		merged := make([]int, 0, len(set))
		for h := range set {
			merged = append(merged, h)
		}
		sort.Ints(merged)
		dst[fi] = merged
	}
}

func confirm() bool {
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func isTTY(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

func termWidth() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 20 {
			return n
		}
	}
	return 80
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstPositive(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}
