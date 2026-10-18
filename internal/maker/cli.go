// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/maker/cli.go
// About: Part of package maker in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package maker

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"acme/wizard/internal/app"
	"acme/wizard/internal/spec"
	"acme/wizard/internal/sys"
)

const usage = `Usage:
  wizard-maker.exe [setup.json]                             open the window
  wizard-maker.exe build [-o out.exe] [-quiet] setup.json   make the setup.exe setup.json describes
  wizard-maker.exe -install [-desktop-shortcut] [-quiet] [-root DIR]   install for this user
  wizard-maker.exe -uninstall [-purge] [-quiet] [-root DIR]           uninstall
  wizard-maker.exe -version                                 print the version
  wizard-maker.exe -smoke FILE [setup.json]                 open, build, capture, report to FILE, close`

// Main is the maker's command line. self is its own exe, the program part of every setup.exe.
func Main(self string, args []string) int {
	if len(args) > 0 && args[0] == "build" {
		return buildCommand(self, args[1:])
	}
	fs := flag.NewFlagSet(app.ExeName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.Bool("version", false, "print the version")
	smoke := fs.String("smoke", "", "open the window, build, write a report to this file, close")
	inst := fs.Bool("install", false, "install for this user (Start menu, Apps & features)")
	uninst := fs.Bool("uninstall", false, "uninstall")
	desktop := fs.Bool("desktop-shortcut", false, "with -install: add a desktop shortcut")
	purge := fs.Bool("purge", false, "with -uninstall: also remove the maker's settings")
	quiet := fs.Bool("quiet", false, "no window or message box")
	root := fs.String("root", "", "sandbox: install under this folder (registry goes to registry.json)")
	err := fs.Parse(aliasQuiet(args))
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
	case err != nil || fs.NArg() > 1:
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
		if s.Output, err = filepath.Abs(*output); err != nil {
			return fail(app.ExitUsage, "%v", err)
		}
	}
	stub, err := os.ReadFile(self)
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=4d9a8286e3ab5880f5f8350b587666f2c2f4d2f453fc8fbdb50d217fccdaa5d0 ====
