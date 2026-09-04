package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/module"
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

			if _, err := loadConfigOrHint(cmd, cfgPath); err != nil {
				return err
			}

			mod, ok := e.Registry.Get(id)
			if !ok {
				return config.Error{Path: cfgPath, Msg: fmt.Sprintf("unknown module %q", id)}
			}

			schema, ok := mod.Manifest.Options[key]
			if !ok {
				return config.Error{
					Path: cfgPath,
					Msg: fmt.Sprintf("unknown option %q for module %q (valid: %s)",
						key, id, strings.Join(optionKeys(mod.Manifest.Options), ", ")),
				}
			}

			typed, err := schema.ParseValue(args[1])
			if err != nil {
				return config.Error{
					Path: cfgPath,
					Msg:  fmt.Sprintf("invalid value for %s.%s: %v", id, key, err),
				}
			}

			if err := config.SetOption(cfgPath, id, key, typed); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "set %s.%s = %s\n", id, key, args[1])
			return nil
		},
	}
}

// optionKeys returns the option keys of a manifest, sorted, for error messages.
func optionKeys(opts map[string]module.OptionSchema) []string {
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
