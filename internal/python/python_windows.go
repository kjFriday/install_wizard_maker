// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/python/python_windows.go
// About: The real Windows registry as a Registry, through package reg.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The real Windows registry as a Registry, through package reg.
//
// The _windows file name limits it to Windows builds, as reg.Subkeys and reg.String are; Find in
// python.go only sees the Registry interface, so its logic is tested with a fake.

package python

import "generic-company/wizard/internal/reg"

// Windows is the real registry.
//
// It reads the 64-bit view of the registry (reg opens keys with KEY_WOW64_64KEY), which is why
// places names the WOW6432Node path for 32-bit installs explicitly.
type Windows struct{}

// Subkeys lists a key's subkeys; a missing key gives none and no error.
func (Windows) Subkeys(root uintptr, path string) ([]string, error) { return reg.Subkeys(root, path) }

// String reads a string value (REG_SZ or REG_EXPAND_SZ, not expanded); ok is false when it is
// missing, empty or of another type.
func (Windows) String(root uintptr, path, name string) (string, bool) {
	return reg.String(root, path, name)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=9c17cf1b72c92c6a493fe1a8dcc31b826c76993597e71d96ca5f70cb50ce84b2 ====
