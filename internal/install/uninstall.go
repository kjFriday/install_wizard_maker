// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/install/uninstall.go
// About: Uninstall removes what install.json lists; DeleteAfterExit removes the uninstaller.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Uninstall removes what install.json lists; DeleteAfterExit removes the uninstaller.
//
// An uninstall.exe (package wizard's RunUninstall) and the maker's own -uninstall reach here after
// reading the marker with ReadMarker. Unlike Apply this is not a transaction: removing is safe to
// repeat, so the order is chosen instead so that a failure half-way can simply be run again (the
// registry entry, which is how the user finds the uninstaller, goes last).
//
// Windows will not delete an exe while it runs, so the uninstaller cannot remove itself. Uninstall
// skips it and returns it in Result.AfterExit; DeleteAfterExit then starts a hidden cmd.exe that
// waits a few seconds for the uninstaller to exit and deletes it and the emptied folders.

package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"generic-company/wizard/internal/spec"
)

// UEnv is what an uninstall needs.
type UEnv struct {
	Places   Places
	Registry Registry
	Self     string                               // the running uninstall.exe: it can only go once it has exited
	Purge    bool                                 // also remove the program's settings folders (the marker's purge entries)
	Progress func(done, total int64, file string) // counts marker entries, not bytes: done of total, skipped ones included; may be nil
}

// Uninstall removes the install in dir that m describes; the caller checks m names its own program.
// Only the marker's shortcuts and files are touched, and a file is removed only if it is still exactly
// as installed (an entry without a hash, such as another installer's, is never removed). The registry
// entry goes last, so a failed uninstall can simply be run again.
//
// Result.Kept lists the entries left because they changed or have no hash. When env.Self lies in
// dir, Result.AfterExit is [Self, dir, and the group folder when it is inside Places.Programs] for
// DeleteAfterExit; otherwise those folders are removed here if empty.
//
// Steps:
//  1. Remove the marker's shortcuts that lie in the Start menu or on the desktop, and their emptied
//     Start-menu group folder.
//  2. Remove each listed file whose SHA-256 still matches, skipping the running uninstaller; a file
//     still in use after removeWait's wait is an error.
//  3. Remove install.json.
//  4. With Purge, remove the purge folders (spec.PurgePath keeps them two levels inside
//     LocalAppData or AppData).
//  5. Delete the registry entry.
//  6. Remove or hand over the install folder and its group folder.
func Uninstall(dir string, m *Marker, env UEnv) (Result, error) {
	var res Result
	// 1. shortcuts: a listed path outside the Start menu and desktop is never touched
	for _, s := range m.Shortcuts {
		if !env.Places.ownsLink(s) {
			continue
		}
		if err := os.Remove(s); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return res, err
		}
		removeEmptyParents(s, env.Places.StartMenu)
	}
	// 2. files; entry drops anything that would reach outside dir
	self, _ := filepath.Abs(env.Self)
	for i, raw := range m.Files {
		e, ok := entry(raw)
		if !ok {
			continue
		}
		if env.Progress != nil {
			env.Progress(int64(i), int64(len(m.Files)), e)
		}
		path := filepath.Join(dir, filepath.FromSlash(e))
		if strings.EqualFold(path, self) {
			continue // it is running; DeleteAfterExit removes it
		}
		// the hash may be keyed by the raw entry or by its normalized form
		want := m.SHA256[raw]
		if want == "" {
			want = m.SHA256[e]
		}
		got, err := fileSum(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		// a folder entry (the Rust sibling's) fails to hash and so is kept too
		if want == "" || err != nil || got != want {
			res.Kept = append(res.Kept, e)
			continue
		}
		if err := removeWait(path); err != nil {
			return res, fmt.Errorf("%w; close it and run the uninstaller again", err)
		}
		removeEmptyParents(path, dir)
	}
	// 3. the marker
	if err := os.Remove(filepath.Join(dir, MarkerName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, err
	}
	// 4. settings folders, only when asked; an entry PurgePath rejects is skipped quietly, and
	// removal is best effort
	if env.Purge {
		for _, p := range m.Purge {
			if path, err := spec.PurgePath(p, env.Places.LocalAppData, env.Places.AppData); err == nil {
				os.RemoveAll(path)
			}
		}
	}
	// 5. the Apps & features entry, last of the steps that can fail
	if err := env.Registry.Delete(m.AppID); err != nil {
		return res, err
	}
	// 6. the folders: the group folder (Programs\<group>) only when it really is inside Programs
	group := filepath.Dir(dir)
	if !within(env.Places.Programs, group) {
		group = ""
	}
	if strings.EqualFold(filepath.Dir(self), dir) {
		res.AfterExit = []string{self, dir}
		if group != "" {
			res.AfterExit = append(res.AfterExit, group)
		}
	} else {
		os.Remove(dir) // only when empty
		if group != "" {
			os.Remove(group)
		}
	}
	if env.Progress != nil {
		env.Progress(int64(len(m.Files)), int64(len(m.Files)), "")
	}
	return res, nil
}

// lockWait is how long removeWait retries a file that is in use. A var so TestRemoveWait can
// shorten it.
var lockWait = 10 * time.Second

// removeWait removes path, retrying while it is in use: a program that started its uninstaller and is
// closing (the wizard maker's Help > Uninstall) still holds its exe for a moment.
// It tries every 200 ms until lockWait has passed. A file already gone counts as removed; any other
// error is retried too, and the last one is returned.
func removeWait(path string) error {
	deadline := time.Now().Add(lockWait)
	for {
		err := os.Remove(path)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// DeleteAfterExit starts a hidden cmd that waits for this process to end, deletes file, then removes
// the folders if they are empty (never with /s: kept files stay). The paths travel in environment
// variables, quoted, so no character in a folder name can change the command.
// dirs are removed in the order given, so list the innermost first. It returns once cmd.exe has
// started; whether the deletes worked is not reported.
//
// How the script works:
//   - cmd's timeout command wants console input, which a hidden cmd lacks, so "ping -n N 127.0.0.1"
//     is the sleep: N echoes to the local host, one a second, wait about N-1 seconds.
//   - It tries del after about 2 s, again after 2 s more if the file is still there, and once more
//     after 5 s: about 9 s in all for the uninstaller to exit.
//   - rd without /s removes only empty folders, so kept files and their folders stay.
//   - cmd expands each %VAR% once, and every one sits inside double quotes. Windows names cannot
//     hold a double quote, so a name's &, ( or ) stays quoted data, and a % in it is not expanded
//     again. Each path is its own variable: GENERIC_COMPANY_DEL, then GENERIC_COMPANY_RD1 to RD9.
//
// Known limitation: the variable suffix is the single character '1'+i, so a tenth folder would get
// ':' and beyond; callers pass at most two (the install folder and its group).
func DeleteAfterExit(file string, dirs ...string) error {
	env := append(os.Environ(), "GENERIC_COMPANY_DEL="+file)
	try := `del /f /q "%GENERIC_COMPANY_DEL%" 2>nul`
	script := `ping -n 3 127.0.0.1 >nul & ` + try +
		` & (if exist "%GENERIC_COMPANY_DEL%" (ping -n 3 127.0.0.1 >nul & ` + try + `))` +
		` & (if exist "%GENERIC_COMPANY_DEL%" (ping -n 6 127.0.0.1 >nul & ` + try + `))`
	for i, d := range dirs {
		v := "GENERIC_COMPANY_RD" + string(rune('1'+i))
		env = append(env, v+"="+d)
		script += ` & rd "%` + v + `%" 2>nul`
	}
	// /d skips the AutoRun commands the registry may set for cmd.exe; /c runs the script and exits.
	// CmdLine is passed to CreateProcess as is, because Go's own argument quoting would escape the
	// script's inner quotes in a way cmd does not undo.
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /c "` + script + `"`, HideWindow: true,
		CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	cmd.Env = env
	cmd.Dir = os.TempDir() // not the folder being removed
	return cmd.Start()
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=fc60502c95afe526bede4893ffae77a0c78093c2e3556a1a5138667298d46f72 ====
