package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/agenticraptor/ghostwriter/internal/buildinfo"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println(buildinfo.String())
			return nil
		},
	}
}
