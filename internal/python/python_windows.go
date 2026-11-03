// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/python/python_windows.go
// About: Part of package python in wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package python

import "generic-company/wizard/internal/reg"

// Windows is the real registry.
type Windows struct{}

// Subkeys lists a key's subkeys.
func (Windows) Subkeys(root uintptr, path string) ([]string, error) { return reg.Subkeys(root, path) }

// String reads a string value.
func (Windows) String(root uintptr, path, name string) (string, bool) {
	return reg.String(root, path, name)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=3cb91d5c63a664e53d11560d38d2b9324b805a425f27f7abc0a2e27dbe88c7be ====
