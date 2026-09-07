#!/bin/sh
# omnishell remove starship --purge: drop the config the module seeded.
# A config the user has since taken ownership of is theirs to keep, but purge
# is explicit intent to erase the module's footprint, so remove it.
rm -f "$OMNISHELL_CONFIG_DIR/starship.toml"
