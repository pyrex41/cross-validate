// Package kernel exposes the Shen rule kernel as an embedded filesystem so
// the xpc binary is self-contained and does not need the kernel/ directory
// present at runtime. The Shen runtime's `open` primitive is rebound at
// startup (see pkg/checker/bridge.go) to consult Read first.
package kernel

import (
	"embed"
	"io/fs"
)

//go:embed *.shen
var FS embed.FS

// Read returns the embedded contents of name (e.g. "check.shen") and
// reports whether the file is part of the embedded kernel.
func Read(name string) ([]byte, bool) {
	b, err := fs.ReadFile(FS, name)
	if err != nil {
		return nil, false
	}
	return b, true
}
