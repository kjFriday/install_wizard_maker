// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/wizard/python.go
// About: Part of package wizard in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package wizard

import (
	"os"

	"generic-company/wizard/internal/python"
)

// findPython is the pythonw.exe of the newest registered CPython of at least min, or "".
var findPython = func(min string) string {
	found, ok := python.Find(python.Windows{}, min, func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	})
	if !ok {
		return ""
	}
	return found.Pythonw
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=407241440222ede1a5b3c537eeb0aabb6af06546405126b164fccbf0a9d292c3 ====
