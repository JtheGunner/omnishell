package cli

import (
	"io"
	"runtime"

	"github.com/JtheGunner/omnishell/internal/buildinfo"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
	"github.com/JtheGunner/omnishell/internal/selfupdate"
	"github.com/spf13/cobra"
)

// updaterFactory builds the self-updater; tests swap it so no command touches
// the network or the real binary.
var updaterFactory = realUpdater

func realUpdater(stdout io.Writer) selfupdate.Updater {
	return selfupdate.Updater{
		Version:     buildinfo.Version,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		GoARM:       platform.GoARMFromBuild(),
		ReleasesURL: selfupdate.ReleasesURL,
		Resolver:    selfupdate.NewRedirectResolver(selfupdate.ReleasesURL + "/latest"),
		Downloader:  pkgmgr.NewHTTPDownloader(),
		Executable:  selfupdate.ResolveExecutable,
		Stdout:      stdout,
	}
}

// SetUpdaterFactoryForTest swaps the self-updater constructor. Passing nil
// restores the real one.
func SetUpdaterFactoryForTest(fn func(stdout io.Writer) selfupdate.Updater) {
	if fn == nil {
		fn = realUpdater
	}
	updaterFactory = fn
}

func newSelfUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Update omnishell to the latest release (for hosts without Homebrew)",
		Long: `Downloads the latest GitHub release for this OS and architecture, verifies the
archive against the release's checksums.txt (SHA-256), and atomically replaces
the running binary. On any failure the old binary stays unchanged.

On a Homebrew installation it points to 'brew upgrade' instead of replacing
itself. It never uses sudo: if the binary's directory is not writable it stops
with an error. Only a strictly newer release triggers an update. A development
build (unknown version) is never replaced.

With --check it only reports whether a newer release exists.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			u := updaterFactory(cmd.OutOrStdout())
			if check, _ := cmd.Flags().GetBool("check"); check {
				return u.Check()
			}
			return u.Update()
		},
	}
	cmd.Flags().Bool("check", false, "only report whether a newer release is available")
	return cmd
}
