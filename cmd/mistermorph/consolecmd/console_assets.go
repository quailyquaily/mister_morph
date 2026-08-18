//go:build !noembedconsole

package consolecmd

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var embeddedConsoleAssets embed.FS

var consoleStaticFS = loadEmbeddedConsoleStaticFS()

func loadEmbeddedConsoleStaticFS() fs.FS {
	staticFS, err := fs.Sub(embeddedConsoleAssets, "static")
	if err != nil {
		return nil
	}
	if err := validateConsoleStaticFS(staticFS); err != nil {
		return nil
	}
	return staticFS
}
