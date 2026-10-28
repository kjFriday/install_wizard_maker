// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/sys/checks_windows.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/sys/checks_windows.go
// About: Part of package sys in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package sys

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	procCreateMutex        = kernel32.NewProc("CreateMutexW")
	procGetDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// Elevated reports whether this process runs as administrator. Setup refuses then: an elevated
// process may belong to another account, and would install into that account's profile.
func Elevated() bool {
	t, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer t.Close()
	var elevation, n uint32
	err = syscall.GetTokenInformation(t, 20, (*byte)(unsafe.Pointer(&elevation)), 4, &n) // TokenElevation
	return err == nil && elevation != 0
}

// Lock takes the session-wide mutex Local\name. ok is false when another process holds it (a second
// setup, or the uninstaller, of the same program).
func Lock(name string) (release func(), ok bool) {
	p, err := syscall.UTF16PtrFromString(`Local\` + name)
	if err != nil {
		return func() {}, true
	}
	h, _, err := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(p)))
	if h == 0 {
		return func() {}, true // no mutex to be had; not a reason to stop
	}
	if errors.Is(err, syscall.ERROR_ALREADY_EXISTS) {
		syscall.CloseHandle(syscall.Handle(h))
		return func() {}, false
	}
	return func() { syscall.CloseHandle(syscall.Handle(h)) }, true
}

// FreeSpace is the space this user may use on the drive of path (or of its nearest existing parent).
func FreeSpace(path string) (uint64, error) {
	for p := path; ; p = filepath.Dir(p) {
		if _, err := os.Stat(p); err == nil {
			u, err := syscall.UTF16PtrFromString(p)
			if err != nil {
				return 0, err
			}
			var free uint64
			if r, _, err := procGetDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(u)), uintptr(unsafe.Pointer(&free)), 0, 0); r == 0 {
				return 0, err
			}
			return free, nil
		}
		if filepath.Dir(p) == p {
			return 0, fmt.Errorf("%s: no such drive", path)
		}
	}
}

// Ask shows a Yes/No question; true for Yes.
func Ask(title, text string) bool {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	r, _, _ := procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x04|0x20) // MB_YESNO | MB_ICONQUESTION
	return r == 6                                                                                        // IDYES
}

// Output is where a role's text goes: the console or pipe there is, else one message box at the end
// (unless quiet), and a log file too when one is asked for.
type Output struct {
	title string
	w     io.Writer
	buf   strings.Builder
	quiet bool
	log   *os.File
}

// NewOutput opens the log (appending) if logPath is set.
func NewOutput(title string, quiet bool, logPath string) (*Output, error) {
	o := &Output{title: title, w: Stdout(), quiet: quiet}
	if logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return o, err
		}
		o.log = f
	}
	return o, nil
}

// Printf writes one line.
func (o *Output) Printf(format string, a ...any) {
	s := fmt.Sprintf(format, a...)
	if o.w != nil {
		io.WriteString(o.w, s+"\n")
	} else {
		o.buf.WriteString(s + "\n")
	}
	if o.log != nil {
		fmt.Fprintf(o.log, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), s)
	}
}

// Done shows what was written in a message box (when there was no console and not quiet) and closes
// the log.
func (o *Output) Done(icon uint32) {
	if o.w == nil && !o.quiet && o.buf.Len() > 0 {
		MessageBox(o.title, strings.TrimSpace(o.buf.String()), icon)
	}
	o.buf.Reset()
	if o.log != nil {
		o.log.Close()
		o.log = nil
	}
}

// SetQuiet turns the final message box off (a window shows the outcome instead).
func (o *Output) SetQuiet(q bool) { o.quiet = q }

// SetTitle changes the message box's title.
func (o *Output) SetTitle(title string) { o.title = title }

// Console reports whether text goes to a console or pipe (so no message box is needed).
func (o *Output) Console() bool { return o.w != nil }

// ==== stamp v1 | author=kjFriday | hmac-sha256=f11d88c999f82f32eb0a240f594eaf79d9bc66749fa33e059cbae5f32ae956d9 ====
