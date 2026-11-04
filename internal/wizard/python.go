// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/wizard/python.go
// About: Finding the Python a Python program's install needs.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Finding the Python a Python program's install needs.
//
// Setup.Check calls findPython when the manifest's program is spec.Python. The search itself is in
// internal/python: CPython installs registered under PEP 514 in HKCU and HKLM, the newest winning.

package wizard

import (
	"os"

	"generic-company/wizard/internal/python"
)

// findPython is the pythonw.exe of the newest registered CPython of at least min, or "".
//
// min is a version such as "3.10" (the manifest's requires.python). An install counts only when its
// pythonw.exe exists as a file. It is a variable rather than a func so a test can swap it; none does
// today.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=304340c7886e1ca5e36b64d57aeb6920f07db2d757d8db6f0bda5b74ce02387f ====
