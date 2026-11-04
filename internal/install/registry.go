// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/install/registry.go
// About: The Apps & features entry as data: registry values and the Registry interface.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The Apps & features entry as data: registry values and the Registry interface.
//
// Windows lists a per-user program in Settings > Apps & features (and Control Panel's Programs and
// Features) when HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Uninstall\<id> exists
// with at least DisplayName and UninstallString. MakePlan builds the values, Apply writes them last of
// all its steps, and Uninstall deletes the key last. The package stays free of Win32 calls here:
// package reg implements Registry twice, as the real key (reg.HKCU) and as root\registry.json in a
// -root sandbox (reg.File), and the tests use an in-memory map.

package install

// Registry value types setup writes (anything else is kept as raw bytes when read).
// The numbers are the Win32 REG_* type codes from winnt.h, as RegSetValueExW takes them.
const (
	RegSZ       = 1 // REG_SZ
	RegExpandSZ = 2 // REG_EXPAND_SZ
	RegDWORD    = 4 // REG_DWORD
)

// Value is one registry value. Type says which of Text, Number or Raw holds the data; the others are
// left zero.
type Value struct {
	Name   string // the value name, such as "DisplayName"
	Type   uint32 // RegSZ, RegExpandSZ, RegDWORD, or another REG_* code read from the key
	Text   string // RegSZ, RegExpandSZ
	Number uint32 // RegDWORD
	Raw    []byte // any other type, as read
}

// Str is a string value.
func Str(name, text string) Value { return Value{Name: name, Type: RegSZ, Text: text} }

// DWord is a 32-bit number value.
func DWord(name string, n uint32) Value { return Value{Name: name, Type: RegDWORD, Number: n} }

// Lookup finds a value by name; ok is false when vals has none. Names are compared exactly, although
// Windows itself treats value names case-insensitively: every caller uses the names MakePlan writes.
func Lookup(vals []Value, name string) (Value, bool) {
	for _, v := range vals {
		if v.Name == name {
			return v, true
		}
	}
	return Value{}, false
}

// Registry is the per-user Uninstall key (HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\<id>),
// or its JSON twin in a -root sandbox. id is the program's app id, the subkey's name.
//
// Read reports found=false with no error when the key is missing. Apply reads the old values first so
// a rollback can Write them back, or Delete the key when there was none.
// Known limitation: reg.HKCU's Write deletes the key and creates it again, so it is not atomic; a
// failure half-way leaves a partial key, which Apply's restore then rewrites.
type Registry interface {
	Read(id string) (vals []Value, found bool, err error)
	Write(id string, vals []Value) error // afterwards the key holds exactly these values
	Delete(id string) error              // no error when it is not there
}

// uninstallKey is the per-user Uninstall key, under HKEY_CURRENT_USER; each program's entry is
// a subkey named by its app id.
const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall`

// UninstallKey is the key under HKEY_CURRENT_USER where id's entry lives. reg.File also uses it,
// prefixed with "HKCU:\", as the key's name in registry.json.
func UninstallKey(id string) string { return uninstallKey + `\` + id }

// ==== stamp v1 | author=kjFriday | hmac-sha256=a0b1f8805d7b9a6a5b6ce0824e21ea2fc04f26fa1eab5e622456115e5a9ff6d0 ====
