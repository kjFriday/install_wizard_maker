// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: main.go
// About: ACME Wizard Maker makes install wizards: one self-contained setup.exe per program, which...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ACME Wizard Maker makes install wizards: one self-contained setup.exe per program, which installs it
// for the current user (no admin rights), lists it in Apps & features and leaves an uninstaller.
//
// One binary, three roles, chosen by the trailer at the end of its own exe: none (the maker), setup
// (a generated install wizard) or uninstall (the uninstaller a setup leaves behind).
package main

import (
	_ "embed"
	"os"
	"runtime"
	"strings"

	"acme/wizard/internal/app"
	"acme/wizard/internal/maker"
	"acme/wizard/internal/payload"
	"acme/wizard/internal/sys"
	"acme/wizard/internal/wizard"
)

//go:embed LICENSE
var license string

func init() {
	// Win32 windows and COM objects belong to the thread that made them.
	runtime.LockOSThread()
	maker.License = license
}

func main() {
	sys.HardenDLLSearch()
	os.Exit(run(os.Args[1:]))
}

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
	defer p.Close()
	switch p.Kind {
	case payload.Setup:
		return wizard.RunSetup(self, p, args)
	case payload.Uninstall:
		return wizard.RunUninstall(self, p, args)
	}
	return maker.Main(self, args)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=0ca5651d18816d30779766869780e00cb37b5366ee0a6b8630b47fc273baad6d ====
