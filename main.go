// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: main.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: main.go
// About: Generic Company Wizard Maker makes install wizards: one self-contained setup.exe per program, which...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Generic Company Wizard Maker makes install wizards: one self-contained setup.exe per program, which installs it
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

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/maker"
	"generic-company/wizard/internal/payload"
	"generic-company/wizard/internal/sys"
	"generic-company/wizard/internal/wizard"
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=d649c37337c06ea7e124bdc034ba88e373110e2969ea6baa585e57c4ef68d753 ====
