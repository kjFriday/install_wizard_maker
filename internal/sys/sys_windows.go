// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/sys/sys_windows.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/sys/sys_windows.go
// About: Package sys holds the small Windows calls every role needs: a safe DLL search path, a...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
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

// HardenDLLSearch makes Windows load DLLs from System32 only, so a DLL planted beside a downloaded
// setup.exe is never loaded. Call it first, before anything loads a DLL.
func HardenDLLSearch() {
	procSetDefaultDllDirectories.Call(0x800) // LOAD_LIBRARY_SEARCH_SYSTEM32
}

// Stdout is where command-line output goes: the inherited standard output when there is one (a pipe, a
// file or a console), else the console of the program that started this one, else nil: a windowsgui
// exe started from Explorer has nowhere to print, so the caller shows a message box instead.
func Stdout() io.Writer {
	if usable(syscall.Stdout) {
		return os.Stdout
	}
	if r, _, _ := procAttachConsole.Call(^uintptr(0)); r != 0 { // ATTACH_PARENT_PROCESS
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
	return err == nil && t != 0 // FILE_TYPE_UNKNOWN
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=5a581a8a4d9e1695d66458d8213f7af4b844438d56efd13985c68a0a871c9f11 ====
