package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// newDocsCmd is a hidden command group used by the release pipeline to
// generate man pages from the command tree. It is not part of the user-facing
// surface — `make man` / GoReleaser call `omnishell docs man <dir>`.
func newDocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "docs",
		Short:  "Generate omnishell's own documentation (build tooling)",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "man <dir>",
		Short: "Write troff man pages for every command into <dir>",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", dir, err)
			}
			header := &doc.GenManHeader{
				Title:   "OMNISHELL",
				Section: "1",
				Source:  "omnishell",
				Manual:  "omnishell manual",
			}
			// GenManTree walks up to the root, so generate from it regardless of
			// where `docs` sits in the tree.
			root := cmd.Root()
			if err := doc.GenManTree(root, header, dir); err != nil {
				return fmt.Errorf("generate man pages: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote man pages to %s\n", dir)
			return nil
		},
	})
	return cmd
}
