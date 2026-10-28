// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/python/python_windows.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/python/python_windows.go
// About: Part of package python in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=5b1887a7573e0f40a880bbc3d219775197b1f28e8250912ac44645774d27e69a ====
