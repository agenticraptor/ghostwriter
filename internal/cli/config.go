package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/agenticraptor/ghostwriter/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the optional configuration file",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "init",
			Short: "Write a documented starter config (never overwrites)",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				path, err := config.Init()
				if err != nil {
					if path != "" {
						return fmt.Errorf("config already exists at %s", path)
					}
					return err
				}
				fmt.Printf("Wrote starter config to %s\n", path)
				return nil
			},
		},
		&cobra.Command{
			Use:   "path",
			Short: "Print the config file path",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				path, err := config.Path()
				if err != nil {
					return err
				}
				fmt.Println(path)
				return nil
			},
		},
		&cobra.Command{
			Use:   "show",
			Short: "Print the effective configuration",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				printConfig(cfg)
				return nil
			},
		},
	)
	return cmd
}

func printConfig(cfg config.Config) {
	fmt.Println("[ai]")
	fmt.Printf("  provider       = %q\n", cfg.AI.Provider)
	fmt.Printf("  model          = %q\n", cfg.AI.Model)
	fmt.Printf("  enabled        = %t\n", cfg.AI.Enabled)
	fmt.Printf("  max_diff_bytes = %d\n", cfg.AI.MaxDiffBytes)
	fmt.Println("[review]")
	fmt.Printf("  against           = %q\n", cfg.Review.Against)
	fmt.Printf("  include_untracked = %t\n", cfg.Review.IncludeUntracked)
}
