// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/maker/self.go
// About: The maker installing and uninstalling itself, by building its own setup.exe.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The maker installing and uninstalling itself, by building its own setup.exe.
//
// The maker is distributed as one bare exe. To put itself in the Start menu and in Apps & features
// it does what it does for any other program: it writes a spec describing itself (SelfSpec), builds
// a setup.exe from it with Build, and runs that setup. Uninstalling runs the uninstall.exe the setup
// left in the install folder.
//
// Callers: cli.go (-install, -uninstall, which wait and pass on the exit code) and the window's Help
// menu in window_help_windows.go (startSelfSetup, selfUninstaller, startUninstaller, which do not
// wait). main sets License; main_test.go builds SelfSpec to check it against setup.json.
//
// Install flow:
//  1. selfSetup makes a temp folder and SelfSpec writes the running exe, LICENSE and icon.ico into it.
//  2. Build packs that folder into setup.exe, using the running exe as the program part (the stub).
//  3. The setup.exe runs (waited for, or started and left), then the temp folder is removed.
//
// Uninstall flow:
//  1. installedDir finds the install folder through the Uninstall key (or registry.json under -root)
//     and the folder's install.json marker.
//  2. runsFrom checks whether this very exe lives in that folder: Windows cannot delete a running exe,
//     so the installed copy must not run its own uninstaller and keep running.
//  3. uninstall.exe runs with the same flags.

package maker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"generic-company/wizard/icon"
	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/install"
	"generic-company/wizard/internal/reg"
	"generic-company/wizard/internal/shell"
	"generic-company/wizard/internal/spec"
)

// License is the wizard maker's own license text; main embeds LICENSE and sets it before anything
// runs. SelfSpec writes it into the install as LICENSE, so the setup's license page shows it.
var License string

// SelfSpec is the setup.json the maker installs itself with, its files written into dir: the running
// exe, the license and the icon. It is the same program as setup.json in the repository describes.
//
// self is the path of the running exe; dir is an existing, empty folder that becomes the spec's
// folder, so every path in the spec is relative to it. Files are written with mode 0644 and any
// read or write error is returned as is.
//
// Steps:
//  1. Read the running exe.
//  2. Write it as app.ExeName, the embedded license as LICENSE and the embedded icon as icon.ico into dir.
//  3. Fill a fresh spec with the maker's identity from package app, icon.ico as its icon, LICENSE
//     as its license, the exe as its only file rule and its program, and setup.exe as the output.
func SelfSpec(self, dir string) (*spec.Spec, error) {
	exe, err := os.ReadFile(self)
	if err != nil {
		return nil, err
	}
	for name, data := range map[string][]byte{app.ExeName: exe, "LICENSE": []byte(License), "icon.ico": icon.ICO} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return nil, err
		}
	}
	s := spec.New(dir)
	s.ID, s.Name, s.Version, s.Publisher, s.Group = app.ID, app.Name, app.Version, app.Publisher, app.Group
	s.Icon, s.License, s.Output = "icon.ico", "LICENSE", "setup.exe"
	s.Files = []spec.FileRule{{From: app.ExeName}}
	s.Run.Program = app.ExeName
	return s, nil
}

// selfSetup builds the maker's own setup.exe in a new temp folder; cleanup removes the folder.
//
// setup is the full path of the built setup.exe. On error the folder is already gone and cleanup is
// nil; on success the caller must call cleanup once setup.exe has finished with the folder.
//
// Steps:
//  1. Make a temp folder named generic-company-wizard-maker-* under the user's temp directory.
//  2. Write the maker's spec and files into it (SelfSpec).
//  3. Read the running exe again as the stub and Build the setup.exe, without a log.
func selfSetup(self string) (setup string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "generic-company-wizard-maker-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	s, err := SelfSpec(self, dir)
	if err == nil {
		var stub []byte
		if stub, err = os.ReadFile(self); err == nil {
			_, err = Build(s, stub, nil)
		}
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return filepath.Join(dir, "setup.exe"), cleanup, nil
}

// installSelf installs this copy of the maker for the current user: it builds its own setup.exe and
// runs it with args (-quiet, -desktop-shortcut, -root ...), waiting for it.
//
// printf receives the setup's own output and any error. The result is the setup's exit code (see
// package app), or app.ExitFailed when the setup could not be built or started. The temp folder is
// removed once the setup exits.
func installSelf(self string, args []string, printf func(string, ...any)) int {
	setup, cleanup, err := selfSetup(self)
	if err != nil {
		printf("%v", err)
		return app.ExitFailed
	}
	defer cleanup()
	return runAndWait(setup, args, printf)
}

// uninstallSelf runs the uninstaller of the installed maker with args, waiting for it. It refuses when
// self runs from the install folder: Windows will not delete a running exe, so the uninstall would stop
// half-way.
//
// root is the -root sandbox folder, or "" for the real per-user install. The result is
// app.ExitOK when nothing is installed (there is nothing to do), app.ExitRefused when self runs from
// the install folder, app.ExitFailed when the folder cannot be found out, and otherwise the
// uninstaller's own exit code.
func uninstallSelf(self, root string, args []string, printf func(string, ...any)) int {
	dir, err := installedDir(root)
	if err != nil {
		printf("%v", err)
		return app.ExitFailed
	}
	if dir == "" {
		printf("%s is not installed for you.", app.Name)
		return app.ExitOK
	}
	uninstaller := filepath.Join(dir, install.UninstallerName)
	if runsFrom(self, dir) {
		printf("This copy runs from %s, which its uninstaller removes: uninstall %s from Apps & features, or run %s.",
			dir, app.Name, uninstaller)
		return app.ExitRefused
	}
	return runAndWait(uninstaller, args, printf)
}

// runsFrom reports whether the exe self lies in dir or a folder under it.
//
// The comparison is case-insensitive, as Windows paths are. A path whose absolute form cannot be
// found counts as outside.
func runsFrom(self, dir string) bool {
	a, err1 := filepath.Abs(self)
	b, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		return false
	}
	// a relative path that does not climb out of dir ("x.exe", `sub\x.exe`) means inside; ".." or
	// `..\...` means outside. A sibling such as "dir 2" gives `..\dir 2\...`, so a plain prefix test on
	// the two strings would wrongly count it as inside.
	rel, err := filepath.Rel(strings.ToLower(b), strings.ToLower(a))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// installedDir is where the maker is installed ("" if it is not), in the -root sandbox if root is set.
//
// With root set, the places are the sandbox layout under root and the registry is root\registry.json
// (reg.File); otherwise they are the user's real known folders and HKEY_CURRENT_USER (reg.HKCU). The
// only error is failing to look up the known folders; install.Existing itself reports none, it just
// finds no folder.
func installedDir(root string) (string, error) {
	var pl install.Places
	var r install.Registry
	if root != "" {
		pl = install.SandboxPlaces(root)
		r = reg.File{Path: filepath.Join(pl.Root, "registry.json")}
	} else {
		var err error
		if pl, err = shell.UserPlaces(); err != nil {
			return "", err
		}
		r = reg.HKCU{}
	}
	dir, _ := install.Existing(r, pl, app.ID, app.Group, app.Name)
	return dir, nil
}

// runAndWait runs exe with args, waits for it, prints its combined output and returns its exit code.
//
// stdout and stderr are collected together and printed once the program ends. A program that ran
// and failed gives its own exit code; one that could not start gives app.ExitFailed after printing
// the error.
func runAndWait(exe string, args []string, printf func(string, ...any)) int {
	cmd := exec.Command(exe, args...)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		printf("%s", string(out))
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		printf("%v", err)
		return app.ExitFailed
	}
	return app.ExitOK
}

// startSelfSetup builds the maker's setup.exe and starts it without waiting (from the window); the
// temp folder goes once it exits.
//
// The setup.exe opens its own wizard window, so the maker's window stays usable meanwhile. A
// goroutine waits for the setup process and then removes the temp folder; if the maker exits
// first, the folder stays behind in the temp directory.
func startSelfSetup(self string) error {
	setup, cleanup, err := selfSetup(self)
	if err != nil {
		return err
	}
	cmd := exec.Command(setup)
	if err := cmd.Start(); err != nil {
		cleanup()
		return err
	}
	go func() {
		cmd.Wait()
		cleanup()
	}()
	return nil
}

// selfUninstaller is the installed maker's uninstall.exe (in the -root sandbox if root is set); inside
// reports whether self runs from that install, so the caller must exit for the uninstall to finish.
//
// It returns an error when the maker is not installed for this user, or when the known folders
// cannot be looked up.
func selfUninstaller(self, root string) (exe string, inside bool, err error) {
	dir, err := installedDir(root)
	if err != nil {
		return "", false, err
	}
	if dir == "" {
		return "", false, fmt.Errorf("%s is not installed for you", app.Name)
	}
	return filepath.Join(dir, install.UninstallerName), runsFrom(self, dir), nil
}

// startUninstaller starts uninstall.exe without waiting.
//
// The uninstaller shows its own window; nothing waits for it, so the maker's window can close and
// let the uninstaller delete the maker's exe.
func startUninstaller(exe string) error {
	return exec.Command(exe).Start()
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=a72199a70a3e4536f72b23dcc10b1534c9296cb4169fae6621c3e758a3971aad ====
