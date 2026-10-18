// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/wizard/uninstall.go
// About: Part of package wizard in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package wizard

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"acme/wizard/internal/app"
	"acme/wizard/internal/install"
	"acme/wizard/internal/payload"
	"acme/wizard/internal/reg"
	"acme/wizard/internal/shell"
	"acme/wizard/internal/sys"
)

type uninstallOptions struct {
	quiet, purge, version bool
	root, log, smoke      string
}

func parseUninstall(args []string) (uninstallOptions, error) {
	var o uninstallOptions
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.quiet, "quiet", false, "no window")
	fs.BoolVar(&o.purge, "purge", false, "also remove the program's settings")
	fs.StringVar(&o.root, "root", "", "sandbox: the folder setup was given with -root")
	fs.StringVar(&o.log, "log", "", "append what happens to this file")
	fs.StringVar(&o.smoke, "smoke", "", "walk the window's pages, write a report to this file, close")
	fs.BoolVar(&o.version, "version", false, "print what this uninstaller removes")
	err := fs.Parse(aliases(args))
	if err == nil && fs.NArg() > 0 {
		err = fmt.Errorf("unexpected %q", fs.Arg(0))
	}
	return o, err
}

const uninstallUsage = `Usage: uninstall.exe [-quiet] [-purge] [-root DIR] [-log FILE]
  -quiet (or /S)   no window
  -purge           also remove the program's settings (never shared data)
  -root DIR        sandbox: the folder setup was given with -root
  -log FILE        append what happens to FILE`

// Uninstaller is one run of an uninstall.exe.
type Uninstaller struct {
	Info     uninstallInfo
	Self     string
	Dir      string
	Marker   *install.Marker
	Places   install.Places
	Registry install.Registry
}

func openUninstaller(self string, p *payload.File, o uninstallOptions) (*Uninstaller, error) {
	b, err := p.ReadAll("uninstall.json", 1<<20)
	if err != nil {
		return nil, err
	}
	u := &Uninstaller{Self: self, Dir: filepath.Dir(self)}
	if err := json.Unmarshal(b, &u.Info); err != nil || u.Info.ID == "" {
		return nil, payload.ErrDamaged
	}
	if o.root != "" {
		u.Places = install.SandboxPlaces(o.root)
		u.Registry = reg.File{Path: filepath.Join(u.Places.Root, "registry.json")}
	} else {
		if sys.Elevated() {
			return u, &install.Refusal{Msg: "Run the uninstaller without \"Run as administrator\": it removes a program installed for you alone."}
		}
		if u.Places, err = shell.UserPlaces(); err != nil {
			return u, err
		}
		u.Registry = reg.HKCU{}
	}
	if u.Marker, err = install.ReadMarker(u.Dir); err != nil {
		return u, err
	}
	if u.Marker == nil || u.Marker.AppID != u.Info.ID {
		return u, &install.Refusal{Msg: fmt.Sprintf("%s holds no install of %s, so there is nothing to remove here.", u.Dir, u.Info.Name)}
	}
	return u, nil
}

// Remove uninstalls; the caller then calls Finish as its very last step.
func (u *Uninstaller) Remove(purge bool, progress func(done, total int64, file string)) (install.Result, error) {
	return install.Uninstall(u.Dir, u.Marker, install.UEnv{Places: u.Places, Registry: u.Registry, Self: u.Self,
		Purge: purge, Progress: progress})
}

// RemoveAfterExit starts the removal of the uninstaller itself and its folders, once this process has exited.
func RemoveAfterExit(res install.Result) {
	if len(res.AfterExit) > 0 {
		install.DeleteAfterExit(res.AfterExit[0], res.AfterExit[1:]...)
	}
}

// RunUninstall is an uninstall.exe's main.
func RunUninstall(self string, p *payload.File, args []string) int {
	o, err := parseUninstall(args)
	out, logErr := sys.NewOutput("Uninstall", o.quiet, o.log)
	if err != nil {
		out.Printf("%v\n\n%s", err, uninstallUsage)
		out.Done(sys.Error)
		return app.ExitUsage
	}
	if logErr != nil {
		out.Printf("log: %v", logErr)
	}
	u, err := openUninstaller(self, p, o)
	if u != nil && u.Info.Name != "" {
		out.SetTitle(u.Info.Name + " Uninstall")
	}
	if err != nil {
		out.Printf("%v", err)
		out.Done(sys.Error)
		return exitCode(err)
	}
	if o.version {
		out.Printf("Uninstaller of %s %s (made with %s)", u.Info.Name, u.Info.Version, u.Info.Maker)
		out.Done(sys.Info)
		return app.ExitOK
	}
	release, ok := sys.Lock("ACME.Wizard." + u.Info.ID)
	if !ok {
		out.Printf("Setup or uninstall of %s is already running.", u.Info.Name)
		out.Done(sys.Warning)
		return app.ExitRefused
	}
	if !o.quiet {
		return uninstallWindow(u, o, out, release)
	}
	res, err := u.Remove(o.purge, nil)
	if err != nil {
		out.Printf("%v", err)
		out.Done(sys.Error)
		release()
		return exitCode(err)
	}
	out.Printf("Removed %s %s.", u.Info.Name, u.Info.Version)
	for _, k := range res.Kept {
		out.Printf("Kept (changed since installed): %s", k)
	}
	out.Done(sys.Info)
	release()
	RemoveAfterExit(res)
	return app.ExitOK
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=dd51e29b5d065ab7f2724e37f466158c0ca5ca0fda28975a47e61b75711b3356 ====
