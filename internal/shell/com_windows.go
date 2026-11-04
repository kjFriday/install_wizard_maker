// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/shell/com_windows.go
// About: Package shell talks to the Windows shell: folders, shortcuts, pickers, opening files.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package shell talks to the Windows shell: folders, shortcuts, pickers, opening files.
//
// It covers known folders, .lnk shortcuts (with the taskbar's AppUserModelID), file and folder
// pickers, and opening files. Most of it is COM, which this file sets up by hand: the module
// uses the standard library only, so there is no COM wrapper to lean on.
//
// COM in short: a COM object is reached through an interface pointer, which points at a pointer to
// a table of function pointers (the vtable). Every interface starts with IUnknown's three methods,
// QueryInterface (0), AddRef (1) and Release (2), followed by its own in the order its header
// declares them, so a method is called by its index with the object pointer as the hidden first
// argument. Objects are made with CoCreateInstance from a class id (CLSID) and the interface id
// (IID) wanted; each returns an HRESULT, negative on failure. A thread must call CoInitializeEx
// before using COM, and shell objects such as the file dialogs want a single-threaded apartment
// (STA), which ties them to that OS thread.
//
// Files: com_windows.go (COM plumbing, GUIDs, strings), dialog_windows.go (pickers and Open),
// link_windows.go (shortcuts and known folders).
package shell

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// The COM runtime (ole32.dll), the shell (shell32.dll) and the ole32 functions used here. DLLs load
// and functions are looked up on first call.
//
// CoTaskMemFree frees strings the shell hands back; PropVariantClear frees a PROPVARIANT's contents
// (link_windows.go uses it for the AppUserModelID property).
var (
	ole32   = syscall.NewLazyDLL("ole32.dll")
	shell32 = syscall.NewLazyDLL("shell32.dll")

	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	procPropVariantClear = ole32.NewProc("PropVariantClear")
)

// GUID is a Windows GUID, laid out as in memory: the first three groups are little-endian numbers,
// the last eight bytes are stored in the order written.
type GUID struct {
	Data1        uint32
	Data2, Data3 uint16
	Data4        [8]byte
}

// MustGUID parses "{XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX}"; only for constants, so it panics.
func MustGUID(s string) GUID {
	h := strings.ReplaceAll(strings.Trim(s, "{}"), "-", "")
	if len(h) != 32 {
		panic("bad GUID " + s)
	}
	// n parses the bits/4 hex digits at i: 8 for Data1, 4 each for Data2 and Data3, 2 per Data4 byte
	n := func(i, bits int) uint64 {
		v, err := strconv.ParseUint(h[i:i+bits/4], 16, bits)
		if err != nil {
			panic("bad GUID " + s)
		}
		return v
	}
	g := GUID{Data1: uint32(n(0, 32)), Data2: uint16(n(8, 16)), Data3: uint16(n(12, 16))}
	for i := range g.Data4 {
		g.Data4[i] = byte(n(16+2*i, 8))
	}
	return g
}

// COM flags and codes (objbase.h, winerror.h): COINIT_APARTMENTTHREADED asks for a single-threaded
// apartment, COINIT_DISABLE_OLE1DDE turns off the old DDE support Microsoft recommends disabling,
// CLSCTX_INPROC_SERVER loads the object from a DLL into this process, and RPC_E_CHANGED_MODE is
// what CoInitializeEx returns when the thread already joined the multi-threaded apartment.
const (
	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4
	clsctxInprocServer      = 0x1
	rpcEChangedMode         = 0x80010106
)

// object is any COM interface pointer: a pointer to its method table.
//
// A *object is the interface pointer itself; its one field is the vtable pointer stored at that
// address. The array length 64 is only an upper bound for indexing, larger than any vtable used
// here; nothing past the real table is read.
type object struct{ vt *[64]uintptr }

// call invokes method i (in vtable order; 0-2 are IUnknown's) and returns the HRESULT.
//
// The object pointer goes first, as the method's "this". The go:uintptrescapes directive below
// tells the compiler that pointers converted to uintptr in args must stay alive (and in place) for
// the duration of the call, as for syscall functions.
//
//go:uintptrescapes
func (o *object) call(i int, args ...uintptr) int32 {
	r, _, _ := syscall.SyscallN(o.vt[i], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return int32(r)
}

// release is IUnknown::Release: it drops this reference, freeing the object with its last one. A
// nil object is ignored, so it can be deferred before a result is known.
func (o *object) release() {
	if o != nil {
		o.call(2)
	}
}

// query is IUnknown::QueryInterface: the same object seen through another interface (iid), with a
// reference of its own that the caller releases.
func (o *object) query(iid *GUID) (*object, error) {
	var out *object
	if hr := o.call(0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))); hr < 0 {
		return nil, hrError("QueryInterface", hr)
	}
	return out, nil
}

// create makes a new in-process object of class clsid and returns its iid interface
// (CoCreateInstance, no aggregation); the caller releases it. COM must be started on this thread.
func create(clsid, iid *GUID) (*object, error) {
	var out *object
	r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if int32(r) < 0 {
		return nil, hrError("CoCreateInstance", int32(r))
	}
	return out, nil
}

// hrError is a failed call's error: what failed and its HRESULT in hex (0x80070005, say), the form
// Microsoft's documentation and search results use.
func hrError(what string, hr int32) error { return fmt.Errorf("%s: HRESULT %#08x", what, uint32(hr)) }

// withCOM runs fn on one OS thread with COM started there (a single-threaded apartment). It can be
// called from any goroutine, and nests.
//
// The goroutine is locked to its OS thread for the call, since COM state and STA objects belong to
// a thread and Go may otherwise move the goroutine. Every object fn creates must be released
// before fn returns.
func withCOM(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	switch hr := int32(r); {
	case hr >= 0: // S_OK, or S_FALSE when already started on this thread: both need CoUninitialize
		defer procCoUninitialize.Call()
	case uint32(hr) != rpcEChangedMode: // a real failure
		return hrError("CoInitializeEx", hr)
	}
	// RPC_E_CHANGED_MODE falls through: COM was started multi-threaded on this thread, which is
	// usable as it is and is not this call's to uninitialize
	return fn()
}

// utf16 is s as a NUL-terminated UTF-16 string for a Win32 call. A string with a NUL inside cannot
// be passed as is, so its NULs are dropped instead of failing.
func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	}
	return p
}

// fromTaskMem copies a string the shell allocated, then frees it.
//
// The shell returns such strings (from IShellItem::GetDisplayName, SHGetKnownFolderPath) in memory
// from CoTaskMemAlloc, which the caller must free with CoTaskMemFree. nil gives "".
func fromTaskMem(p *uint16) string {
	if p == nil {
		return ""
	}
	// count the UTF-16 units up to the NUL; unsafe.Add steps 2 bytes per unit
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), 2*n)) != 0 {
		n++
	}
	s := syscall.UTF16ToString(unsafe.Slice(p, n))
	procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return s
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=805273a8604f327bca73f5a28ac59b63687d7a13addea9a9485276ac39cf8e5a ====
