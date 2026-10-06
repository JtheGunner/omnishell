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
# -buildvcs=false: the container checkout trips git's "dubious ownership"
# guard (exit 128), and this smoke binary doesn't need a version stamp.
mkdir -p "$sandbox/bin"
go build -buildvcs=false -o "$sandbox/bin/omnishell" ./cmd/omnishell
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

# A module whose package the system repositories cannot provide must fall back
# to its git fallback instead of degrading. The package name exists in no
# repository and the fallback clones a local repo, so this needs no network and
# no toolchain on any distro.
fallback_src="$sandbox/e2e-fallback-src"
git init -q "$fallback_src"
echo ok > "$fallback_src/marker"
git -C "$fallback_src" add marker
git -C "$fallback_src" -c user.name=e2e -c user.email=e2e@example.invalid commit -q -m init
module_dir="$HOME/.config/omnishell/modules/e2e-fallback"
mkdir -p "$module_dir"
cat > "$module_dir/manifest.toml" <<MANIFEST
platforms = ["macos", "linux"]
shells    = ["bash"]

[module]
id          = "e2e-fallback"
name        = "e2e fallback"
description = "Exercises the git fallback for an unavailable package"
version     = "1.0.0"
schema      = 1

[packages]
brew   = ["omnishell-e2e-unavailable"]
apt    = ["omnishell-e2e-unavailable"]
dnf    = ["omnishell-e2e-unavailable"]
pacman = ["omnishell-e2e-unavailable"]
zypper = ["omnishell-e2e-unavailable"]
apk    = ["omnishell-e2e-unavailable"]

[[packages.fallback]]
type = "git"
repo = "file://$fallback_src"
dest = "{{.VendorDir}}/e2e-fallback"
MANIFEST
echo 'export OMNISHELL_E2E_FALLBACK=1' > "$module_dir/bash.tmpl"
omnishell enable e2e-fallback
omnishell apply --yes
test -f "$HOME/.config/omnishell/vendor/e2e-fallback/marker" || { echo "git fallback not used for unavailable package"; exit 1; }
grep -q 'omnishell:e2e-fallback' "$HOME/.config/omnishell/init.bash" || { echo "e2e-fallback not applied"; exit 1; }
omnishell doctor
omnishell remove fzf --yes
! grep -q 'omnishell:fzf' "$HOME/.config/omnishell/init.bash" || { echo "fzf survived remove"; exit 1; }
omnishell uninstall --yes
! grep -q 'omnishell' "$HOME/.bashrc" || { echo "uninstall left markers"; exit 1; }
echo "E2E OK"
