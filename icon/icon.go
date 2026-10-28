// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: icon/icon.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: icon/icon.go
// About: Package icon embeds the wizard maker's pixel icon; README.md has the credit and every...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package icon embeds the wizard maker's pixel icon; README.md has the credit and every file's SHA-256.
package icon

import _ "embed"

// ICO is app.ico: 16, 32 and 48 px as 32-bit BMP, 256 px as PNG.
//
//go:embed app.ico
var ICO []byte

// ==== stamp v1 | author=kjFriday | hmac-sha256=a5a1f681a54963e42bd2812581c2403a3ba52fa9fa758c8e35f894b037e3e7e3 ====
