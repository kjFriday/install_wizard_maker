// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/install/registry.go
// About: Part of package install in wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package install

// Registry value types setup writes (anything else is kept as raw bytes when read).
const (
	RegSZ       = 1 // REG_SZ
	RegExpandSZ = 2 // REG_EXPAND_SZ
	RegDWORD    = 4 // REG_DWORD
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

// uninstallKey is the per-user Uninstall key, under HKEY_CURRENT_USER; each program's entry is
// a subkey named by its app id.
const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall`

// UninstallKey is the key under HKEY_CURRENT_USER where id's entry lives.
func UninstallKey(id string) string { return uninstallKey + `\` + id }

// ==== stamp v1 | author=kjFriday | hmac-sha256=7b06eaa5dc6eabf69fcc6fa68cbb592f41e73cbb680f7f86ab35ee50f8c7a1d6 ====
