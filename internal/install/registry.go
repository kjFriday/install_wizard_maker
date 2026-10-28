// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/install/registry.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/install/registry.go
// About: Part of package install in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package install

// Registry value types setup writes (anything else is kept as raw bytes when read).
const (
	RegSZ       = 1
	RegExpandSZ = 2
	RegDWORD    = 4
)

// Value is one registry value.
type Value struct {
	Name   string
	Type   uint32
	Text   string // RegSZ, RegExpandSZ
	Number uint32 // RegDWORD
	Raw    []byte // any other type, as read
}

// Str is a string value.
func Str(name, text string) Value { return Value{Name: name, Type: RegSZ, Text: text} }

// DWord is a 32-bit number value.
func DWord(name string, n uint32) Value { return Value{Name: name, Type: RegDWORD, Number: n} }

// Lookup finds a value by name.
func Lookup(vals []Value, name string) (Value, bool) {
	for _, v := range vals {
		if v.Name == name {
			return v, true
		}
	}
	return Value{}, false
}

// Registry is the per-user Uninstall key (HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\<id>),
// or its JSON twin in a -root sandbox.
type Registry interface {
	Read(id string) (vals []Value, found bool, err error)
	Write(id string, vals []Value) error // afterwards the key holds exactly these values
	Delete(id string) error              // no error when it is not there
}

// UninstallKey is the key under HKEY_CURRENT_USER where id's entry lives.
func UninstallKey(id string) string { return uninstallKey + `\` + id }

// ==== stamp v1 | author=kjFriday | hmac-sha256=dc7767e02284855877dfc2524d76f46e45dd75235cac3126826b3ce11a105288 ====
