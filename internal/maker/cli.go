// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/maker/cli.go
// About: The maker's command line: Main picks build, -install, -uninstall or the window.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The maker's command line: Main picks build, -install, -uninstall or the window.
//
// The maker is a windowsgui exe, so it has no console of its own: sys.NewOutput prints to the
// console or pipe of whoever started it, or collects the text for one message box when there is
// none (or for none at all with -quiet). Exit codes are app's: 0 OK, 1 failed, 2 bad usage or a bad
// setup.json; -install and -uninstall return the exit code of the setup or uninstaller they ran.

package maker

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/spec"
	"generic-company/wizard/internal/sys"
)

// usage is the help text printed after a usage error, by Main and by the build command. README
// documents the same flags.
const usage = `Usage:
  wizard-maker.exe [setup.json]                             open the window
  wizard-maker.exe build [-o out.exe] [-quiet] setup.json   make the setup.exe setup.json describes
  wizard-maker.exe -install [-desktop-shortcut] [-quiet] [-root DIR]   install for this user
  wizard-maker.exe -uninstall [-purge] [-quiet] [-root DIR]           uninstall
  wizard-maker.exe -version                                 print the version
  wizard-maker.exe -smoke FILE [setup.json]                 open, build, capture, report to FILE, close`

// Main is the maker's command line. self is its own exe, the program part of every setup.exe.
// args are the arguments after the exe name; the result is the process exit code.
//
// Steps:
//  1. "build" as the first argument hands the rest to buildCommand.
//  2. Parse the flags (/S counts as -quiet).
//  3. Pick by flag, in this order: a usage error, -version, -install, -uninstall.
//  4. Otherwise open the window, with the optional setup.json argument and -smoke file.
//
// -install and -uninstall pass -quiet and -root on to the setup or uninstaller they run.
func Main(self string, args []string) int {
	if len(args) > 0 && args[0] == "build" {
		return buildCommand(self, args[1:])
	}
	fs := flag.NewFlagSet(app.ExeName, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are reported through o with usage, not by package flag
	version := fs.Bool("version", false, "print the version")
	smoke := fs.String("smoke", "", "open the window, build, write a report to this file, close")
	inst := fs.Bool("install", false, "install for this user (Start menu, Apps & features)")
	uninst := fs.Bool("uninstall", false, "uninstall")
	desktop := fs.Bool("desktop-shortcut", false, "with -install: add a desktop shortcut")
	purge := fs.Bool("purge", false, "with -uninstall: also remove the maker's settings")
	quiet := fs.Bool("quiet", false, "no window or message box")
	root := fs.String("root", "", "sandbox: install under this folder (registry goes to registry.json)")
	err := fs.Parse(aliasQuiet(args))
	// made after parsing so -quiet is known; no log file, and without a console o falls back to a
	// message box
	o, _ := sys.NewOutput(app.Name, *quiet, "")
	pass := func(flags ...string) []string { // the flags the setup or uninstaller gets too
		if *quiet {
			flags = append(flags, "-quiet")
		}
		if *root != "" {
			flags = append(flags, "-root", *root)
		}
		return flags
	}
	switch {
	case err != nil || fs.NArg() > 1: // the window takes at most one setup.json
		if err == nil {
			err = fmt.Errorf("unexpected %q", fs.Arg(1))
		}
		o.Printf("%v\n\n%s", err, usage)
		o.Done(sys.Error)
		return app.ExitUsage
	case *version:
		o.Printf("%s %s", app.Name, app.Version)
		o.Done(sys.Info)
		return app.ExitOK
	case *inst:
		var flags []string
		if *desktop {
			flags = append(flags, "-desktop-shortcut")
		}
		code := installSelf(self, pass(flags...), o.Printf)
		o.Done(sys.Info)
		return code
	case *uninst:
		var flags []string
		if *purge {
			flags = append(flags, "-purge")
		}
		code := uninstallSelf(self, *root, pass(flags...), o.Printf)
		o.Done(sys.Info)
		return code
	}
	return Window(self, fs.Arg(0), *smoke)
}

// buildCommand is "wizard-maker.exe build [-o out.exe] [-quiet] setup.json": it builds the setup.exe
// without opening the window and returns the exit code.
// A bad command line or setup.json exits app.ExitUsage (2), with every problem listed indented under
// the file's name; any other failure exits app.ExitFailed (1). Flags must come before setup.json,
// because package flag stops at the first non-flag argument.
func buildCommand(self string, args []string) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := fs.String("o", "", "where to write the setup.exe (default: output in setup.json)")
	quiet := fs.Bool("quiet", false, "no message box")
	err := fs.Parse(args)
	o, _ := sys.NewOutput(app.Name, *quiet, "")
	fail := func(code int, format string, a ...any) int {
		o.Printf(format, a...)
		o.Done(sys.Error)
		return code
	}
	if err != nil || fs.NArg() != 1 {
		if err == nil && fs.NArg() > 1 {
			err = errors.New("flags go before setup.json")
		} else if err == nil {
			err = errors.New("which setup.json?")
		}
		return fail(app.ExitUsage, "%v\n\n%s", err, usage)
	}
	s, err := spec.Load(fs.Arg(0))
	if err != nil {
		return fail(app.ExitUsage, "%v", err)
	}
	if *output != "" {
		// -o is relative to the current folder, unlike output in setup.json, which is relative to
		// setup.json's folder
		if s.Output, err = filepath.Abs(*output); err != nil {
			return fail(app.ExitUsage, "%v", err)
		}
	}
	stub, err := os.ReadFile(self) // the maker's own exe becomes the setup's program part
	if err != nil {
		return fail(app.ExitFailed, "%v", err)
	}
	if _, err := Build(s, stub, o.Printf); err != nil {
		var se *SpecError
		if errors.As(err, &se) {
			return fail(app.ExitUsage, "%s has problems:\n  %s", filepath.Base(fs.Arg(0)), strings.ReplaceAll(err.Error(), "\n", "\n  "))
		}
		return fail(app.ExitFailed, "Build failed: %v", err)
	}
	o.Done(sys.Info)
	return app.ExitOK
}

// aliasQuiet accepts /S for -quiet, as the setup and uninstaller do.
// /S (any case) is the silent switch common Windows installers use, so scripts written for them work.
// It returns a new slice; args is not changed.
func aliasQuiet(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.EqualFold(a, "/S") {
			a = "-quiet"
		}
		out[i] = a
	}
	return out
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=430d8ac6ad6d42762173aa6f4b4480816f7e7aca8ee6a02b61bb75c0b6533d57 ====
