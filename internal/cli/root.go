package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// NewRootCmd builds the omnishell root command with every subcommand attached.
func NewRootCmd(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "omnishell",
		Short:         "Modular, declarative terminal configuration for macOS and Linux",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().Bool("verbose", false, "verbose output")

	root.AddCommand(newVersionCmd(stdout))
	root.AddCommand(newInitCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newEnableCmd())
	root.AddCommand(newDisableCmd())
	root.AddCommand(newSetCmd())
	return root
}

// Execute runs omnishell with the given args and returns an exit code.
func Execute(args []string, stdout, stderr io.Writer) int {
	root := NewRootCmd(stdout, stderr)
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		if _, werr := io.WriteString(stderr, "error: "+err.Error()+"\n"); werr != nil {
			_ = werr
		}
	}
	return ClassifyError(err)
}
