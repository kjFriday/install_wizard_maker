// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: main.go
// About: Generic Company Wizard Maker makes install wizards: one setup.exe per program.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Generic Company Wizard Maker makes install wizards: one setup.exe per program.
//
// Each setup.exe is self-contained: it installs its program for the current user (no admin rights),
// lists it in Apps & features and leaves an uninstaller.
//
// One binary, three roles, chosen by the trailer at the end of its own exe: none (the maker), setup
// (a generated install wizard) or uninstall (the uninstaller a setup leaves behind). A setup.exe is
// this very program with a payload appended (package payload), so every role ships in every exe.
//
// The main flow:
//  1. init locks the main goroutine to its OS thread and hands the embedded LICENSE to the maker.
//  2. main restricts DLL loading to System32, then runs run and exits with its code.
//  3. run opens its own exe's payload and dispatches by Kind: wizard.RunSetup, wizard.RunUninstall,
//     or maker.Main when there is no payload.
//
// Every role returns the shared exit codes of package app: 0 done, 1 failed, 2 usage, 3 cancelled,
// 4 refused.
package main

import (
	_ "embed"
	"os"
	"runtime"
	"strings"

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/maker"
	"generic-company/wizard/internal/payload"
	"generic-company/wizard/internal/sys"
	"generic-company/wizard/internal/wizard"
)

// license is the wizard maker's own license text, compiled in so the maker can install itself with
// it (maker.SelfSpec writes it out as the setup's LICENSE).
//
//go:embed LICENSE
var license string

// init prepares the process before main runs.
//
// Steps:
//  1. Lock the main goroutine to the main OS thread. init runs on the main goroutine, so the windows
//     every role opens later are all made, and served, on this one thread.
//  2. Hand the embedded license to package maker.
func init() {
	// Win32 windows and COM objects belong to the thread that made them.
	runtime.LockOSThread()
	maker.License = license
}

// main hardens DLL loading first, so a DLL planted beside a downloaded setup.exe is never loaded,
// then runs the role and exits with its code.
func main() {
	sys.HardenDLLSearch()
	os.Exit(run(os.Args[1:]))
}

// run is the program for args (the arguments after the exe name); it returns the exit code.
//
// Steps:
//  1. Find its own exe; without it nothing can be read, so say so in a message box.
//  2. Open the exe's payload. On an error (a cut-short download, a damaged file) the role is unknown
//     and no flags have been parsed, so it scans args for -quiet, --quiet or /S by hand and reports
//     the error as a setup would.
//  3. Dispatch by the payload's Kind: Setup and Uninstall to package wizard, anything else (None,
//     the maker itself) to maker.Main.
func run(args []string) int {
	self, err := os.Executable()
	if err != nil {
		sys.MessageBox(app.Name, err.Error(), sys.Error)
		return app.ExitFailed
	}
	p, err := payload.Open(self)
	if err != nil {
		quiet := false
		for _, a := range args {
			quiet = quiet || a == "-quiet" || a == "--quiet" || strings.EqualFold(a, "/S")
		}
		out, _ := sys.NewOutput("Setup", quiet, "")
		out.Printf("%v", err)
		out.Done(sys.Error)
		return app.ExitFailed
	}
	defer p.Close() // the wizards read their files from the payload while they run
	switch p.Kind {
	case payload.Setup:
		return wizard.RunSetup(self, p, args)
	case payload.Uninstall:
		return wizard.RunUninstall(self, p, args)
	}
	return maker.Main(self, args)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=1ecba2707972b7f7db9eaa3ab757f1734fb3ec011f46f0d6ec8b44f09160a754 ====
