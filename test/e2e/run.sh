#!/bin/sh
# Build omnishell, run a real init+enable+apply+doctor cycle, assert the
# init file and rc block exist and doctor is clean. Intended to run as root
# inside a minimal distro container.
set -eu

go build -o /usr/local/bin/omnishell ./cmd/omnishell

export HOME=/root
touch "$HOME/.bashrc"

omnishell init
omnishell enable history
omnishell enable completion
omnishell enable fzf
omnishell apply --yes

test -f "$HOME/.config/omnishell/init.bash" || { echo "init.bash missing"; exit 1; }
grep -q '>>> omnishell >>>' "$HOME/.bashrc" || { echo "rc block missing"; exit 1; }
grep -q 'omnishell:fzf' "$HOME/.config/omnishell/init.bash" || { echo "fzf not applied (package install failed?)"; exit 1; }

# Capture the snapshot just taken by the apply above, then roll back to it
# and confirm doctor reports no drift against the restored (pre-apply) state.
snapshot=$(omnishell rollback | head -n1 | awk '{print $1}')
omnishell rollback --to "$snapshot" --yes
omnishell doctor
omnishell remove fzf --yes
! grep -q 'omnishell:fzf' "$HOME/.config/omnishell/init.bash" || { echo "fzf survived remove"; exit 1; }
omnishell uninstall --yes
! grep -q 'omnishell' "$HOME/.bashrc" || { echo "uninstall left markers"; exit 1; }
echo "E2E OK"
