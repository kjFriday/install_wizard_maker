// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/python/python_windows.go
// About: Part of package python in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package python

import "acme/wizard/internal/reg"

// Windows is the real registry.
type Windows struct{}

// Subkeys lists a key's subkeys.
func (Windows) Subkeys(root uintptr, path string) ([]string, error) { return reg.Subkeys(root, path) }

// String reads a string value.
func (Windows) String(root uintptr, path, name string) (string, bool) {
	return reg.String(root, path, name)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=90e78ed7b7f86d932da147bb5a297fda5cf0ed088e1fa5759ca4f033a92b2be9 ====
