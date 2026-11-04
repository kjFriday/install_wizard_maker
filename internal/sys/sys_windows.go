// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/sys/sys_windows.go
// About: Package sys holds the small Windows calls every role needs, through kernel32 and user32.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package sys holds the small Windows calls every role needs, through kernel32 and user32.
//
// They are a safe DLL search path, a console for command-line output, message boxes, and the
// checks setup runs before it changes anything. main.go calls HardenDLLSearch first thing; the
// maker's command line, the wizard and the uninstaller report through Output; the wizard and the
// uninstaller call Elevated, Lock and FreeSpace before they touch the disk.
//
// Files: sys_windows.go (DLL search, console, message boxes) and checks_windows.go (elevation, the
// single-instance mutex, free space, and Output). Types: Output.
package sys

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

// The system DLLs and the functions this package calls. Lazy DLLs load, and functions are looked
// up, on first call; kernel32 and user32 are known DLLs, always taken from System32.
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	procSetDefaultDllDirectories = kernel32.NewProc("SetDefaultDllDirectories")
	procAttachConsole            = kernel32.NewProc("AttachConsole")
	procMessageBox               = user32.NewProc("MessageBoxW")
)

// SetDefaultDllDirectories, AttachConsole and GetFileType values.
//
// LOAD_LIBRARY_SEARCH_SYSTEM32 (libloaderapi.h), ATTACH_PARENT_PROCESS (wincon.h) and
// FILE_TYPE_UNKNOWN (fileapi.h), which GetFileType returns for a handle that is not a file, pipe
// or console.
const (
	loadLibrarySearchSystem32 = 0x0800
	attachParentProcess       = ^uintptr(0) // (DWORD)-1
	fileTypeUnknown           = 0
)

// HardenDLLSearch makes Windows load DLLs from System32 only, so a DLL planted beside a downloaded
// setup.exe is never loaded. Call it first, before anything loads a DLL.
//
// A downloaded setup.exe usually runs from the Downloads folder, where a hostile page may have
// saved a DLL with a system DLL's name; by default Windows looks in the exe's own folder first
// ("DLL preloading"). The result is ignored: on failure the default search order stays.
func HardenDLLSearch() {
	procSetDefaultDllDirectories.Call(loadLibrarySearchSystem32)
}

// Stdout is where command-line output goes: the inherited standard output when there is one (a pipe, a
// file or a console), else the console of the program that started this one, else nil: a windowsgui
// exe started from Explorer has nowhere to print, so the caller shows a message box instead.
//
// A windowsgui exe started from cmd gets no standard handles, so it attaches to cmd's console
// (AttachConsole with ATTACH_PARENT_PROCESS) and writes to it through CONOUT$, the console's
// output device. The prompt may already be back by then, so the output can appear after it.
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

// usable reports whether h is a real file, pipe or console handle (not unset, invalid or of
// unknown type).
func usable(h syscall.Handle) bool {
	if h == 0 || h == syscall.InvalidHandle {
		return false
	}
	t, err := syscall.GetFileType(h)
	return err == nil && t != fileTypeUnknown
}

// Message box icons: MB_ICONINFORMATION, MB_ICONWARNING and MB_ICONERROR (winuser.h).
const (
	Info    = 0x40
	Warning = 0x30
	Error   = 0x10
)

// MessageBox shows a plain message box with no owner window.
//
// icon is Info, Warning or Error. It blocks until the user closes the box. A NUL inside title or
// text makes that string nil: Windows then shows no text, or its default "Error" title.
func MessageBox(title, text string, icon uint32) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), uintptr(icon))
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=5717e5d35e27a662fe68f6b55725253a4af3be9020ed00af6e6372fcf6dd7c03 ====
