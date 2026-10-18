// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/shell/com_windows.go
// About: Package shell talks to the Windows shell: known folders, .lnk shortcuts (with the...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package shell talks to the Windows shell: known folders, .lnk shortcuts (with the taskbar's
// AppUserModelID), file and folder pickers, and opening files.
package shell

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var (
	ole32   = syscall.NewLazyDLL("ole32.dll")
	shell32 = syscall.NewLazyDLL("shell32.dll")

	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	procPropVariantClear = ole32.NewProc("PropVariantClear")
)

// GUID is a Windows GUID.
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

const (
	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4
	clsctxInprocServer      = 0x1
	rpcEChangedMode         = 0x80010106
)

// object is any COM interface pointer: a pointer to its method table.
type object struct{ vt *[64]uintptr }

// call invokes method i (in vtable order; 0-2 are IUnknown's) and returns the HRESULT.
//
//go:uintptrescapes
func (o *object) call(i int, args ...uintptr) int32 {
	r, _, _ := syscall.SyscallN(o.vt[i], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return int32(r)
}

func (o *object) release() {
	if o != nil {
		o.call(2)
	}
}

// query is IUnknown::QueryInterface.
func (o *object) query(iid *GUID) (*object, error) {
	var out *object
	if hr := o.call(0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out))); hr < 0 {
		return nil, hrError("QueryInterface", hr)
	}
	return out, nil
}

func create(clsid, iid *GUID) (*object, error) {
	var out *object
	r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if int32(r) < 0 {
		return nil, hrError("CoCreateInstance", int32(r))
	}
	return out, nil
}

func hrError(what string, hr int32) error { return fmt.Errorf("%s: HRESULT %#08x", what, uint32(hr)) }

// withCOM runs fn on one OS thread with COM started there (a single-threaded apartment). It can be
// called from any goroutine, and nests.
func withCOM(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	switch hr := int32(r); {
	case hr >= 0: // S_OK, or S_FALSE when already started on this thread: both need CoUninitialize
		defer procCoUninitialize.Call()
	case uint32(hr) != rpcEChangedMode: // started as multi-threaded: usable as it is
		return hrError("CoInitializeEx", hr)
	}
	return fn()
}

func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	}
	return p
}

// fromTaskMem copies a string the shell allocated, then frees it.
func fromTaskMem(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), 2*n)) != 0 {
		n++
	}
	s := syscall.UTF16ToString(unsafe.Slice(p, n))
	procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return s
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=c54ecbe526359cf6913c0ba50ddd6bcc5b3ab4c2c004d2d44b352f5a3c33f52a ====
