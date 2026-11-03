// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/wizard/python.go
// About: Part of package wizard in wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=31c98021e00cac1fa28605ad780b1d9608b41ac3871c48dff85e256e6a5bd549 ====
