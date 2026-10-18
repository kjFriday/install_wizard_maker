// ACME Wizard Maker makes install wizards: one self-contained setup.exe per program, which installs it
// for the current user (no admin rights), lists it in Apps & features and leaves an uninstaller.
package main

import (
	"fmt"
	"os"
	"runtime"

	"acme/wizard/internal/app"
)

func init() {
	// Win32 windows and COM objects belong to the thread that made them.
	runtime.LockOSThread()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-version" {
		fmt.Println(app.Name, app.Version)
		return
	}
	fmt.Println(app.Title)
}
