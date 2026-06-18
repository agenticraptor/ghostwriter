package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/agenticraptor/ghostwriter/internal/config"
	"github.com/agenticraptor/ghostwriter/internal/gitdiff"
)

func newDoctorCmd() *cobra.Command {
	var repo string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check your environment and configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runDoctor(cmd.Context(), repo)
			return nil
		},
	}
	cmd.Flags().StringVarP(&repo, "repo", "r", ".", "Repository to check")
	return cmd
}

func runDoctor(ctx context.Context, repo string) {
	ok := func(label, detail string) { fmt.Printf("  \033[32m✓\033[0m %-22s %s\n", label, detail) }
	warn := func(label, detail string) { fmt.Printf("  \033[33m!\033[0m %-22s %s\n", label, detail) }
	bad := func(label, detail string) { fmt.Printf("  \033[31m✗\033[0m %-22s %s\n", label, detail) }

	fmt.Print("ghostwriter doctor\n\n")

	if out, err := exec.CommandContext(ctx, "git", "--version").Output(); err == nil {
		ok("git", strings.TrimSpace(string(out)))
	} else {
		bad("git", "not found on PATH — ghostwriter needs git")
	}

	if root, err := gitdiff.Root(ctx, repo); err == nil {
		ok("repository", root)
	} else {
		bad("repository", fmt.Sprintf("%q is not a git repository", repo))
	}

	switch {
	case os.Getenv("ANTHROPIC_API_KEY") != "":
		ok("ai provider", "anthropic (ANTHROPIC_API_KEY set)")
	case os.Getenv("OPENAI_API_KEY") != "":
		ok("ai provider", "openai (OPENAI_API_KEY set)")
	default:
		warn("ai provider", "no API key set — will try local Ollama, or use --no-ai for offline grouping")
	}

	if path, err := config.Path(); err == nil {
		if _, statErr := os.Stat(path); statErr == nil {
			ok("config", path)
		} else {
			warn("config", "none yet ("+path+") — run `ghostwriter config init` to create one")
		}
	}

	fmt.Print("\nTip: run `ghostwriter` in a repo your agent just edited to start a review.\n")
}
