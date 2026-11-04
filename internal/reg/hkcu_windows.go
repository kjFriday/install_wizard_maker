// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/reg/hkcu_windows.go
// About: The real registry via advapi32: the Uninstall key and reads for package python.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The real registry via advapi32: the Uninstall key and reads for package python.
//
// Go's standard library has no registry package and the module uses the standard library only (no
// golang.org/x/sys/windows/registry), so this file calls the Win32 Reg* functions directly
// through syscall. Those functions return their error code (0 is ERROR_SUCCESS) instead of setting
// GetLastError, which status turns into an error. Strings cross the boundary as UTF-16.
//
// Every key is opened with KEY_WOW64_64KEY, so a 32-bit build would still see the 64-bit view.
// HKEY_CURRENT_USER\Software is shared by both views anyway; the flag matters for the
// HKEY_LOCAL_MACHINE reads package python makes.

package reg

import (
	"errors"
	"sort"
	"syscall"
	"unsafe"

	"generic-company/wizard/internal/install"
)

// Root keys: the predefined handles HKEY_CURRENT_USER and HKEY_LOCAL_MACHINE (winreg.h), passed
// where a function expects an open key handle.
const (
	CurrentUser  = 0x80000001
	LocalMachine = 0x80000002
)

// Win32 registry access rights, error codes, options and limits.
const (
	keyRead      = 0x20019            // KEY_READ
	keyWrite     = 0x20006            // KEY_WRITE
	key64        = 0x0100             // KEY_WOW64_64KEY
	errNotFound  = syscall.Errno(2)   // ERROR_FILE_NOT_FOUND
	errNoMore    = syscall.Errno(259) // ERROR_NO_MORE_ITEMS
	optionNormal = 0                  // REG_OPTION_NON_VOLATILE
	keyNameMax   = 256                // a key name is at most 255 characters, plus its NUL
)

// advapi32.dll and the registry functions used here, the wide-string (W) variants. The DLL loads
// and each function is looked up on first call.
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

// open opens root\path with the given access (plus the 64-bit view) via RegOpenKeyExW and returns
// its handle; the caller closes it with closeKey. A missing key gives errNotFound.
func open(root uintptr, path string, access uint32) (uintptr, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var h uintptr
	r, _, _ := procOpenKey.Call(root, uintptr(unsafe.Pointer(p)), 0, uintptr(access|key64), uintptr(unsafe.Pointer(&h)))
	return h, status(r)
}

// closeKey closes a key handle from open or RegCreateKeyExW; its result is ignored.
func closeKey(h uintptr) { procCloseKey.Call(h) }

// HKCU is the real Uninstall key of the current user. Base, when set, replaces
// Software\Microsoft\Windows\CurrentVersion\Uninstall (tests use a throwaway key).
//
// Writing under HKEY_CURRENT_USER needs no administrator rights, which is what lets setup install
// per user without elevation.
type HKCU struct{ Base string }

// path is id's key under HKEY_CURRENT_USER: Base\id, or the standard Uninstall key.
func (k HKCU) path(id string) string {
	if k.Base != "" {
		return k.Base + `\` + id
	}
	return install.UninstallKey(id)
}

// Read returns the key's values, sorted by name.
//
// found is false, without error, when the key is missing. REG_SZ and REG_EXPAND_SZ come back as
// Text (not expanded), REG_DWORD as Number, anything else as Raw bytes.
//
// Steps:
//  1. Open the key for reading.
//  2. Ask RegQueryInfoKeyW for the value count and the longest value name and data, so one buffer
//     size fits every value.
//  3. Enumerate the values by index with RegEnumValueW, decoding each by its type.
//  4. Sort by name, so the order does not depend on the registry's.
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
	// RegQueryInfoKeyW(key, class, classLen, reserved, subKeys, maxSubKeyLen, maxClassLen, values,
	// maxValueNameLen, maxValueLen, securityDescriptorLen, lastWriteTime): only the value count and
	// the longest name and data are asked for; the zeros are NULL pointers
	r, _, _ := procQueryInfoKey.Call(h, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&nameMax)), uintptr(unsafe.Pointer(&dataMax)), 0, 0)
	if err := status(r); err != nil {
		return nil, false, err
	}
	var out []install.Value
	for i := uint32(0); i < count; i++ {
		name := make([]uint16, nameMax+1) // the longest name, plus its NUL
		data := make([]byte, dataMax+2)   // the longest data, plus room for a UTF-16 NUL
		// in: the buffer sizes (name in characters, data in bytes); out: the lengths written, the name
		// without its NUL
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
				// REG_DWORD is a little-endian 32-bit number
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
//
// It is not atomic: the old key is deleted first, so a failure part-way leaves the key missing or
// with only some values.
//
// Steps:
//  1. Delete the key and any subkeys (no error if it is missing).
//  2. Create it again with RegCreateKeyExW (non-volatile, write access, 64-bit view).
//  3. Set each value with RegSetValueExW: strings as UTF-16 with their NUL, DWORDs as 4
//     little-endian bytes, anything else as its Raw bytes.
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
			// view the UTF-16 units (NUL included, as REG_SZ expects) as bytes without copying
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

// deleteTree removes HKEY_CURRENT_USER\path with all its values and subkeys (RegDeleteTreeW); a
// missing key is not an error. It ignores Base: path is the full key path.
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
//
// RegDeleteKeyW fails on a key that still has subkeys, so a shared parent is only removed once the
// last test key under it is gone.
func DeleteKey(root uintptr, path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, _ := procDeleteKey.Call(root, uintptr(unsafe.Pointer(p)))
	return status(r)
}

// Subkeys lists the names of a key's subkeys (none when the key is missing).
//
// It enumerates by index with RegEnumKeyExW until ERROR_NO_MORE_ITEMS. On another error it returns
// the names read so far with the error.
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
		name := make([]uint16, keyNameMax)
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
//
// ok is also false for a value of another type, an empty value, or any error. REG_EXPAND_SZ is
// returned as stored, with its %VARIABLES% not expanded.
//
// Steps:
//  1. Open the key for reading.
//  2. Call RegQueryValueExW with no buffer to learn the type and size in bytes.
//  3. Call it again to fill a buffer of that size (allocated two bytes larger, though only size is
//     passed and used).
//  4. Decode the UTF-16, stopping at the first NUL; a stored string need not end in one.
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

// utf16Text decodes registry string data: little-endian UTF-16 bytes, up to the first NUL. An odd
// last byte is dropped.
func utf16Text(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return syscall.UTF16ToString(u)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=7ea067f5c0b942f40af25f8c31fc41e9fa9f75e09353270319c288521e21ece5 ====
