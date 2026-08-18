//go:build noembedconsole

package consolecmd

import "io/fs"

var consoleStaticFS fs.FS
