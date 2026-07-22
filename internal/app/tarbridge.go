package app

import (
	"io"

	"github.com/alanwnuczko/local-mesh/internal/transfer"
)

// tarFolderImpl delegates to transfer.TarFolder for the confirm pre-pass.
// The app package imports transfer only through this bridge so the dependency
// is explicit and easy to locate.
func tarFolderImpl(path string, w io.Writer) error {
	return transfer.TarFolder(path, w)
}
