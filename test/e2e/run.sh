#!/bin/sh
# Build omnishell and run a real init -> enable -> apply -> doctor -> rollback ->
# remove -> uninstall cycle against a throwaway $HOME, asserting the init file and
# rc block are created and torn down as expected. Runs unprivileged on macOS and
# as root inside a minimal distro container. Package installs still touch the real
# system, so run this only in disposable environments.
set -eu

sandbox=$(mktemp -d)
trap 'rm -rf "$sandbox"' EXIT

# Build into a sandbox bin on PATH so we don't need write access to
# /usr/local/bin (not writable for the unprivileged macOS runner).
mkdir -p "$sandbox/bin"
go build -o "$sandbox/bin/omnishell" ./cmd/omnishell
PATH="$sandbox/bin:$PATH"
export PATH

# Isolate all omnishell state under a throwaway home. omnishell resolves the home
# dir purely from $HOME; unset XDG_CONFIG_HOME so config can't escape the sandbox.
export HOME="$sandbox/home"
unset XDG_CONFIG_HOME
mkdir -p "$HOME"
touch "$HOME/.bashrc"

omnishell init
omnishell enable history
omnishell enable completion
omnishell enable fzf
omnishell apply --yes

test -f "$HOME/.config/omnishell/init.bash" || { echo "init.bash missing"; exit 1; }
grep -q '>>> omnishell >>>' "$HOME/.bashrc" || { echo "rc block missing"; exit 1; }
grep -q 'omnishell:fzf' "$HOME/.config/omnishell/init.bash" || { echo "fzf not applied (package install failed?)"; exit 1; }

# Capture the snapshot just taken by the apply above, then roll back to it.
# rollback never touches config.toml, so once it removes init.bash and the
# lockfile, config.toml still requests the enabled modules while nothing is
# applied on disk — doctor is expected to report drift here (exit 3), which
# proves the rollback actually took effect. Assert that expected drift, then
# re-apply so the pre-existing `doctor` call right after this block still
# finds a clean, converged state as it did before this step was added.
snapshot=$(omnishell rollback | head -n1 | awk '{print $1}')
omnishell rollback --to "$snapshot" --yes
test ! -f "$HOME/.config/omnishell/init.bash" || { echo "init.bash survived rollback"; exit 1; }
set +e
omnishell doctor
rollback_doctor_exit=$?
set -e
test "$rollback_doctor_exit" -eq 3 || { echo "doctor after rollback exited $rollback_doctor_exit, want 3 (drift expected: config still enables modules but rollback removed lock+files)"; exit 1; }
omnishell apply --yes
omnishell doctor
omnishell remove fzf --yes
! grep -q 'omnishell:fzf' "$HOME/.config/omnishell/init.bash" || { echo "fzf survived remove"; exit 1; }
omnishell uninstall --yes
! grep -q 'omnishell' "$HOME/.bashrc" || { echo "uninstall left markers"; exit 1; }
echo "E2E OK"
