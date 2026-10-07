package cli

import (
	"fmt"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/spf13/cobra"
)

func newSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <module>.<key> <value>",
		Short: "Set a module option in the config",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, cfgPath, _, err := buildEngine(cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			dot := strings.LastIndex(args[0], ".")
			if dot < 0 {
				return config.Error{Path: cfgPath, Msg: "expected <module>.<key>"}
			}
			id, key := args[0][:dot], args[0][dot+1:]

			editor := modedit.Editor{Engine: e, CfgPath: cfgPath}
			if err := editor.SetOption(id, key, args[1]); err != nil {
				return hintIfUninitialised(cmd, err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "set %s.%s = %s\n", id, key, args[1])
			return nil
		},
	}
}
