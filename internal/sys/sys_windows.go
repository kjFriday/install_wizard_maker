// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/sys/sys_windows.go
// About: Package sys holds the small Windows calls every role needs: a safe DLL search path, a...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package sys holds the small Windows calls every role needs: a safe DLL search path, a console for
// command-line output, message boxes, and the checks setup runs before it changes anything.
package sys

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	procSetDefaultDllDirectories = kernel32.NewProc("SetDefaultDllDirectories")
	procAttachConsole            = kernel32.NewProc("AttachConsole")
	procMessageBox               = user32.NewProc("MessageBoxW")
)

// SetDefaultDllDirectories, AttachConsole and GetFileType values.
const (
	loadLibrarySearchSystem32 = 0x0800
	attachParentProcess       = ^uintptr(0) // (DWORD)-1
	fileTypeUnknown           = 0
)

// HardenDLLSearch makes Windows load DLLs from System32 only, so a DLL planted beside a downloaded
// setup.exe is never loaded. Call it first, before anything loads a DLL.
func HardenDLLSearch() {
	procSetDefaultDllDirectories.Call(loadLibrarySearchSystem32)
}

// Stdout is where command-line output goes: the inherited standard output when there is one (a pipe, a
// file or a console), else the console of the program that started this one, else nil: a windowsgui
// exe started from Explorer has nowhere to print, so the caller shows a message box instead.
func Stdout() io.Writer {
	if usable(syscall.Stdout) {
		return os.Stdout
	}
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r != 0 {
		if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			return f
		}
	}
	return nil
}

func usable(h syscall.Handle) bool {
	if h == 0 || h == syscall.InvalidHandle {
		return false
	}
	t, err := syscall.GetFileType(h)
	return err == nil && t != fileTypeUnknown
}

// Message box icons.
const (
	Info    = 0x40
	Warning = 0x30
	Error   = 0x10
)

// MessageBox shows a plain message box with no owner window.
func MessageBox(title, text string, icon uint32) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), uintptr(icon))
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=95cb97bc4df071c0b51c99fef7a5535fa6badff7886d74fea8d3f3ed864f7fef ====
