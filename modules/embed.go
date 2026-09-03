// Package modules embeds the built-in module folders.
package modules

import (
	"embed"
	"io/fs"
)

//go:embed all:builtin
var builtin embed.FS

// FS returns the built-in modules filesystem, rooted so each entry is a
// module directory. It is empty until Phase 9 populates modules/builtin/<id>/.
func FS() fs.FS {
	sub, err := fs.Sub(builtin, "builtin")
	if err != nil {
		panic(err)
	}
	return sub
}
