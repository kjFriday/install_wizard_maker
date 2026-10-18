// ACME Wizard Maker makes install wizards: one self-contained setup.exe per program, which installs it
// for the current user (no admin rights), lists it in Apps & features and leaves an uninstaller.
//
// One binary, three roles, chosen by the trailer at the end of its own exe: none (the maker), setup
// (a generated install wizard) or uninstall (the uninstaller a setup leaves behind).
package main

import (
	"os"
	"runtime"

	"acme/wizard/internal/app"
	"acme/wizard/internal/maker"
	"acme/wizard/internal/sys"
)

func init() {
	// Win32 windows and COM objects belong to the thread that made them.
	runtime.LockOSThread()
}

func main() {
	sys.HardenDLLSearch()
	self, err := os.Executable()
	if err != nil {
		sys.MessageBox(app.Name, err.Error(), sys.Error)
		os.Exit(app.ExitFailed)
	}
	os.Exit(maker.Main(self, os.Args[1:]))
}
