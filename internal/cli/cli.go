// Package cli wires the ghostwriter command-line interface together.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/agenticraptor/ghostwriter/internal/buildinfo"
)

// Execute runs the root command and returns a process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	var o reviewOptions

	cmd := &cobra.Command{
		Use:   "ghostwriter",
		Short: "See what your AI agent changed — as a story — before you accept it",
		Long: `ghostwriter reads the changes in your working tree (whatever your AI coding
agent just did), narrates them grouped by intent in plain English, flags the
risky bits, and lets you accept or reject each intent with one keystroke —
applying your rejections back to the working tree.

Run with no arguments to review the current repository. With a terminal it opens
the interactive reviewer; piped or with --print it prints the narrated review
and never touches your files. It works with zero setup: without an API key it
falls back to deterministic, offline grouping.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       buildinfo.Version,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReview(cmd.Context(), o)
		},
	}
	addReviewFlags(cmd, &o)
	cmd.SetVersionTemplate("{{.Name}} {{.Version}}\n")

	cmd.AddCommand(
		newReviewCmd(),
		newConfigCmd(),
		newDoctorCmd(),
		newVersionCmd(),
	)
	return cmd
}

func newReviewCmd() *cobra.Command {
	var o reviewOptions
	cmd := &cobra.Command{
		Use:           "review",
		Short:         "Review pending changes (same as running with no subcommand)",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReview(cmd.Context(), o)
		},
	}
	addReviewFlags(cmd, &o)
	return cmd
}

func addReviewFlags(cmd *cobra.Command, o *reviewOptions) {
	f := cmd.Flags()
	f.StringVarP(&o.repo, "repo", "r", ".", "Path to the repository to review")
	f.StringVar(&o.against, "against", "", "Git ref to compare the working tree against (default HEAD)")
	f.BoolVar(&o.staged, "staged", false, "Review only staged changes (index vs HEAD)")
	f.BoolVar(&o.noUntracked, "no-untracked", false, "Skip new, untracked files")
	f.BoolVar(&o.noAI, "no-ai", false, "Skip the model; use deterministic offline grouping")
	f.StringVar(&o.provider, "provider", "", "LLM provider: anthropic | openai | ollama")
	f.StringVar(&o.model, "model", "", "Model name (defaults to the provider's default)")
	f.StringVarP(&o.format, "format", "f", "", "Output format: term | plain | markdown | json")
	f.StringVarP(&o.output, "output", "o", "", "Write the review to a file instead of the terminal")
	f.BoolVar(&o.print, "print", false, "Print the narrated review without the interactive UI (never modifies files)")
	f.BoolVarP(&o.yes, "yes", "y", false, "Apply rejections without the confirmation prompt")
	f.BoolVar(&o.noColor, "no-color", false, "Disable colored output")
	f.IntVar(&o.maxBytes, "max-bytes", 0, "Cap how much diff text is sent to the model")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "Suppress progress messages")
}
