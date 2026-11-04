// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/sys/checks_windows.go
// About: Pre-flight checks (elevation, single instance, free space) and Output, a role's text sink.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Pre-flight checks (elevation, single instance, free space) and Output, a role's text sink.
//
// The wizard and the uninstaller run Elevated and Lock before they change anything, and the wizard
// checks FreeSpace against the manifest's size. Every role that prints (the maker's command line,
// setup, uninstall) creates one Output, writes lines to it with Printf, and calls Done at the end.

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

// The kernel32 functions the checks call (the DLL is declared in sys_windows.go).
var (
	procCreateMutex        = kernel32.NewProc("CreateMutexW")
	procGetDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// GetTokenInformation and MessageBox values.
//
// TokenElevation is the TOKEN_INFORMATION_CLASS member (winnt.h) whose answer is a TOKEN_ELEVATION,
// one DWORD that is nonzero when elevated; MB_YESNO, MB_ICONQUESTION and IDYES are from winuser.h.
const (
	tokenElevation = 20
	mbYesNo        = 0x04
	mbIconQuestion = 0x20
	idYes          = 6
)

// Elevated reports whether this process runs as administrator. Setup refuses then: an elevated
// process may belong to another account, and would install into that account's profile.
//
// It is false when the token cannot be read, so a failed check never blocks an install.
func Elevated() bool {
	t, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer t.Close()
	var elevation, n uint32
	err = syscall.GetTokenInformation(t, tokenElevation, (*byte)(unsafe.Pointer(&elevation)), 4, &n)
	return err == nil && elevation != 0
}

// Lock takes the session-wide mutex Local\name. ok is false when another process holds it (a second
// setup, or the uninstaller, of the same program).
//
// A named mutex is a kernel object other processes can open by name; the Local\ prefix puts the
// name in this logon session's namespace, so other users' sessions do not collide. Holding the
// handle is the lock: release closes it, and Windows closes it anyway when the process exits.
// CreateMutexW opens an existing mutex instead of failing, and reports that with
// ERROR_ALREADY_EXISTS. When no mutex can be made (or name holds a NUL), ok is true with a no-op
// release, so a failed lock never blocks setup.
func Lock(name string) (release func(), ok bool) {
	p, err := syscall.UTF16PtrFromString(`Local\` + name)
	if err != nil {
		return func() {}, true
	}
	// default security, not initially owned: existence alone is the signal
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
//
// The result is in bytes. It is GetDiskFreeSpaceExW's "available to caller" figure, which honours
// disk quotas, so it can be less than the drive's free space. The install folder usually does not
// exist yet, hence the walk up to a parent that does; a path with no existing ancestor (a missing
// drive) is an error.
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
//
// The box has no owner window and blocks until answered. Nothing in the module calls it at present.
func Ask(title, text string) bool {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	r, _, _ := procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbYesNo|mbIconQuestion)
	return r == idYes
}

// Output is where a role's text goes: the console or pipe there is, else one message box at the end
// (unless quiet), and a log file too when one is asked for.
//
// It is not safe for use from several goroutines at once.
type Output struct {
	title string          // the message box's title
	w     io.Writer       // Stdout(), or nil when there is no console
	buf   strings.Builder // lines saved for the message box when w is nil
	quiet bool            // no message box at the end
	log   *os.File        // the -log file, or nil
}

// NewOutput opens the log (appending) if logPath is set.
//
// The console is found once, here. When the log cannot be opened the Output still comes back,
// usable without a log, along with the error.
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
//
// The line goes to the console, or is saved for Done's message box, and is also appended to the
// log with a local-time timestamp. Write errors are ignored.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=9eafd173195fc91f6b8df732394f8bc1d5b2f2785b3ab7cec070120ed7bf8faa ====
