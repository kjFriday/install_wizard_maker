// Package icon embeds the wizard maker's pixel icon; README.md has the credit and every file's SHA-256.
package icon

import _ "embed"

// ICO is app.ico: 16, 32 and 48 px as 32-bit BMP, 256 px as PNG.
//
//go:embed app.ico
var ICO []byte
