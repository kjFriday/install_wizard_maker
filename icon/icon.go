// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: icon/icon.go
// About: Package icon embeds the wizard maker's pixel icon; README.md has the credit and every...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package icon embeds the wizard maker's pixel icon; README.md has the credit and every file's SHA-256.
package icon

import _ "embed"

// ICO is app.ico: 16, 32 and 48 px as 32-bit BMP, 256 px as PNG.
//
//go:embed app.ico
var ICO []byte

// ==== stamp v1 | author=kjFriday | hmac-sha256=40d3126e57f27c852d0d14bfa2f5042c521403ebb90f77484fe4e3bfee2d260f ====
