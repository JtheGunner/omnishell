package cli

import (
	"fmt"
	"io"

	"github.com/JtheGunner/omnishell/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersionCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the omnishell version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(stdout, buildinfo.String())
			return err
		},
	}
}
