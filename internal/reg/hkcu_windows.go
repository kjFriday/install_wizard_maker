// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/reg/hkcu_windows.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/reg/hkcu_windows.go
// About: Part of package reg in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package reg

import (
	"errors"
	"sort"
	"syscall"
	"unsafe"

	"generic-company/wizard/internal/install"
)

// Root keys.
const (
	CurrentUser  = 0x80000001
	LocalMachine = 0x80000002
)

const (
	keyRead      = 0x20019
	keyWrite     = 0x20006
	key64        = 0x0100 // KEY_WOW64_64KEY
	errNotFound  = syscall.Errno(2)
	errNoMore    = syscall.Errno(259)
	optionNormal = 0
)

var (
	advapi32         = syscall.NewLazyDLL("advapi32.dll")
	procOpenKey      = advapi32.NewProc("RegOpenKeyExW")
	procCreateKey    = advapi32.NewProc("RegCreateKeyExW")
	procCloseKey     = advapi32.NewProc("RegCloseKey")
	procSetValue     = advapi32.NewProc("RegSetValueExW")
	procQueryValue   = advapi32.NewProc("RegQueryValueExW")
	procEnumValue    = advapi32.NewProc("RegEnumValueW")
	procEnumKey      = advapi32.NewProc("RegEnumKeyExW")
	procQueryInfoKey = advapi32.NewProc("RegQueryInfoKeyW")
	procDeleteTree   = advapi32.NewProc("RegDeleteTreeW")
	procDeleteKey    = advapi32.NewProc("RegDeleteKeyW")
)

// status turns a registry function's return value (an error code, not GetLastError) into an error.
func status(r uintptr) error {
	if r == 0 {
		return nil
	}
	return syscall.Errno(r)
}

func open(root uintptr, path string, access uint32) (uintptr, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var h uintptr
	r, _, _ := procOpenKey.Call(root, uintptr(unsafe.Pointer(p)), 0, uintptr(access|key64), uintptr(unsafe.Pointer(&h)))
	return h, status(r)
}

func closeKey(h uintptr) { procCloseKey.Call(h) }

// HKCU is the real Uninstall key of the current user. Base, when set, replaces
// Software\Microsoft\Windows\CurrentVersion\Uninstall (tests use a throwaway key).
type HKCU struct{ Base string }

func (k HKCU) path(id string) string {
	if k.Base != "" {
		return k.Base + `\` + id
	}
	return install.UninstallKey(id)
}

// Read returns the key's values, sorted by name.
func (k HKCU) Read(id string) ([]install.Value, bool, error) {
	h, err := open(CurrentUser, k.path(id), keyRead)
	if errors.Is(err, errNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer closeKey(h)
	var count, nameMax, dataMax uint32
	r, _, _ := procQueryInfoKey.Call(h, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&nameMax)), uintptr(unsafe.Pointer(&dataMax)), 0, 0)
	if err := status(r); err != nil {
		return nil, false, err
	}
	var out []install.Value
	for i := uint32(0); i < count; i++ {
		name := make([]uint16, nameMax+1)
		data := make([]byte, dataMax+2)
		nameLen, dataLen := uint32(len(name)), uint32(len(data))
		var typ uint32
		var dp uintptr
		if len(data) > 0 {
			dp = uintptr(unsafe.Pointer(&data[0]))
		}
		r, _, _ := procEnumValue.Call(h, uintptr(i), uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nameLen)),
			0, uintptr(unsafe.Pointer(&typ)), dp, uintptr(unsafe.Pointer(&dataLen)))
		if err := status(r); errors.Is(err, errNoMore) {
			break
		} else if err != nil {
			return nil, false, err
		}
		v := install.Value{Name: syscall.UTF16ToString(name[:nameLen]), Type: typ}
		data = data[:dataLen]
		switch typ {
		case install.RegSZ, install.RegExpandSZ:
			v.Text = utf16Text(data)
		case install.RegDWORD:
			if len(data) >= 4 {
				v.Number = uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
			}
		default:
			v.Raw = append([]byte(nil), data...)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, true, nil
}

// Write replaces the key with exactly these values.
func (k HKCU) Write(id string, vals []install.Value) error {
	if err := k.Delete(id); err != nil {
		return err
	}
	p, err := syscall.UTF16PtrFromString(k.path(id))
	if err != nil {
		return err
	}
	var h uintptr
	r, _, _ := procCreateKey.Call(CurrentUser, uintptr(unsafe.Pointer(p)), 0, 0, optionNormal,
		uintptr(keyWrite|key64), 0, uintptr(unsafe.Pointer(&h)), 0)
	if err := status(r); err != nil {
		return err
	}
	defer closeKey(h)
	for _, v := range vals {
		name, err := syscall.UTF16PtrFromString(v.Name)
		if err != nil {
			return err
		}
		var data []byte
		switch v.Type {
		case install.RegSZ, install.RegExpandSZ:
			u, err := syscall.UTF16FromString(v.Text)
			if err != nil {
				return err
			}
			data = unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), 2*len(u))
		case install.RegDWORD:
			n := v.Number
			data = []byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24)}
		default:
			data = v.Raw
		}
		var dp uintptr
		if len(data) > 0 {
			dp = uintptr(unsafe.Pointer(&data[0]))
		}
		r, _, _ := procSetValue.Call(h, uintptr(unsafe.Pointer(name)), 0, uintptr(v.Type), dp, uintptr(len(data)))
		if err := status(r); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes the key; no error when it is not there.
func (k HKCU) Delete(id string) error { return k.deleteTree(k.path(id)) }

func (HKCU) deleteTree(path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, _ := procDeleteTree.Call(CurrentUser, uintptr(unsafe.Pointer(p)))
	if err := status(r); err != nil && !errors.Is(err, errNotFound) {
		return err
	}
	return nil
}

// DeleteKey removes an empty key (tests tidy up their throwaway base with it).
func DeleteKey(root uintptr, path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, _ := procDeleteKey.Call(root, uintptr(unsafe.Pointer(p)))
	return status(r)
}

// Subkeys lists the names of a key's subkeys (none when the key is missing).
func Subkeys(root uintptr, path string) ([]string, error) {
	h, err := open(root, path, keyRead)
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer closeKey(h)
	var out []string
	for i := 0; ; i++ {
		name := make([]uint16, 256)
		n := uint32(len(name))
		r, _, _ := procEnumKey.Call(h, uintptr(i), uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&n)), 0, 0, 0, 0)
		if err := status(r); errors.Is(err, errNoMore) {
			return out, nil
		} else if err != nil {
			return out, err
		}
		out = append(out, syscall.UTF16ToString(name[:n]))
	}
}

// String reads a string value ("" for the key's default value); ok is false when it is missing.
func String(root uintptr, path, name string) (string, bool) {
	h, err := open(root, path, keyRead)
	if err != nil {
		return "", false
	}
	defer closeKey(h)
	n, _ := syscall.UTF16PtrFromString(name)
	var typ, size uint32
	r, _, _ := procQueryValue.Call(h, uintptr(unsafe.Pointer(n)), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
	if status(r) != nil || (typ != install.RegSZ && typ != install.RegExpandSZ) || size == 0 {
		return "", false
	}
	data := make([]byte, size+2)
	r, _, _ = procQueryValue.Call(h, uintptr(unsafe.Pointer(n)), 0, uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)))
	if status(r) != nil {
		return "", false
	}
	return utf16Text(data[:size]), true
}

func utf16Text(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return syscall.UTF16ToString(u)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=ea5431705fc48cb14ed9219eeaa87e22692d1d0cc591e82d387c383bb52c1812 ====
