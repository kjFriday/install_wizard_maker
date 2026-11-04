// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/wizard/uninstall.go
// About: The uninstaller's command line and its quiet run: what an uninstall.exe does on start.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The uninstaller's command line and its quiet run: what an uninstall.exe does on start.
//
// A setup writes uninstall.exe into the install folder: the same program as the maker, with a small
// payload holding uninstall.json (uninstallInfo, made by makeUninstaller in setup.go). main sees the
// payload's Kind and calls RunUninstall here. Apps & features runs it through the UninstallString
// that setup registered (with -quiet for QuietUninstallString, and -root for a sandboxed install).
//
// The main flow (RunUninstall):
//  1. Parse the flags (parseUninstall); /S counts as -quiet.
//  2. openUninstaller reads uninstall.json, picks the real or sandboxed places and registry, and
//     checks that the folder beside the exe holds an install of this very program (its install.json).
//  3. Take the per-program lock that setup also takes, so the two never run at once.
//  4. Without -quiet, hand over to the window (uninstall_windows.go). With -quiet, remove at once
//     (Remove, which is install.Uninstall), report what was kept, and as the very last step start the
//     removal of the exe itself (RemoveAfterExit), which cannot happen while it runs.
//
// Types: uninstallOptions (the parsed flags), Uninstaller (one run).

package wizard

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/install"
	"generic-company/wizard/internal/payload"
	"generic-company/wizard/internal/reg"
	"generic-company/wizard/internal/shell"
	"generic-company/wizard/internal/sys"
)

// uninstallOptions are an uninstall.exe's flags, as parseUninstall reads them; uninstallUsage
// describes each.
type uninstallOptions struct {
	quiet, purge, version bool
	root, log, smoke      string
}

// parseUninstall reads an uninstall.exe's arguments (after the exe name) into uninstallOptions.
//
// The flag set neither prints nor exits: errors come back for RunUninstall to show with
// uninstallUsage. /S is first rewritten to -quiet (aliases, setup.go), and any argument left over
// after the flags is an error, since the uninstaller takes none.
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

// uninstallUsage is the help printed after a usage error. Like setupUsage, it leaves out -smoke and
// -version.
const uninstallUsage = `Usage: uninstall.exe [-quiet] [-purge] [-root DIR] [-log FILE]
  -quiet (or /S)   no window
  -purge           also remove the program's settings (never shared data)
  -root DIR        sandbox: the folder setup was given with -root
  -log FILE        append what happens to FILE`

// Uninstaller is one run of an uninstall.exe.
type Uninstaller struct {
	Info     uninstallInfo    // which program this uninstaller may remove (its uninstall.json)
	Self     string           // the running uninstall.exe
	Dir      string           // the install folder: the one the exe lies in
	Marker   *install.Marker  // the folder's install.json: what setup installed there
	Places   install.Places   // the user's real folders, or the -root sandbox's
	Registry install.Registry // HKCU, or the sandbox's registry.json
}

// openUninstaller prepares an uninstall: self is the running exe, p its payload, o its flags.
//
// Errors: the payload's error when uninstall.json cannot be read; payload.ErrDamaged when it is not
// valid JSON or has no id; an
// *install.Refusal when run elevated (the program was installed for one user, and an elevated
// process may be another account) or when the folder holds no install of this program. After
// uninstall.json has been read the Uninstaller comes back even with an error, so the caller can name
// the program in its message.
//
// Steps:
//  1. Read uninstall.json (at most 1 MiB) and take Dir from the exe's own path.
//  2. With -root, use the sandbox places and its registry.json; otherwise refuse when elevated and
//     use the user's real folders and HKCU.
//  3. Read the folder's marker; refuse unless it names this program's app id, so an uninstaller
//     copied elsewhere never removes another program's files.
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

// Remove uninstalls; the caller then calls RemoveAfterExit as its very last step.
//
// purge also removes the program's settings folders (the marker's purge list). progress, which may
// be nil, gets marker entries done of total and the entry at hand. The Result lists the files kept
// (changed since installed) and, in AfterExit, the exe and folders that can only go once this
// process has exited. See install.Uninstall for the order of removal.
func (u *Uninstaller) Remove(purge bool, progress func(done, total int64, file string)) (install.Result, error) {
	return install.Uninstall(u.Dir, u.Marker, install.UEnv{Places: u.Places, Registry: u.Registry, Self: u.Self,
		Purge: purge, Progress: progress})
}

// RemoveAfterExit starts the removal of the uninstaller itself and its folders, once this process has exited.
//
// A running exe cannot delete itself on Windows, so install.DeleteAfterExit starts a hidden cmd.exe
// that waits, deletes the file and removes the folders if empty. res.AfterExit is [exe, folders...]
// and empty when there is nothing to hand over. Its error (cmd.exe not starting) is ignored: the
// uninstall itself is done, and only the uninstaller and empty folders would stay.
func RemoveAfterExit(res install.Result) {
	if len(res.AfterExit) > 0 {
		install.DeleteAfterExit(res.AfterExit[0], res.AfterExit[1:]...)
	}
}

// RunUninstall is an uninstall.exe's main.
//
// self is the running exe, p its opened payload, args the arguments after the exe name. The result
// is the process exit code (app.Exit...; exitCode in setup.go maps errors to it).
//
// Steps:
//  1. Parse the flags and open the output (message box or console, and the -log file); a usage
//     error prints uninstallUsage. A log that cannot be opened is reported, not fatal.
//  2. openUninstaller; name the program in the output's title as soon as it is known.
//  3. -version prints which program this uninstaller removes, and stops.
//  4. Take the lock shared with setup; a second run is refused.
//  5. Without -quiet the window takes over, lock release included.
//  6. With -quiet: Remove, report, release the lock, and hand the exe to RemoveAfterExit last.
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
	release, ok := sys.Lock(lockName(u.Info.ID))
	if !ok {
		out.Printf("Setup or uninstall of %s is already running.", u.Info.Name)
		out.Done(sys.Warning)
		return app.ExitRefused
	}
	// release is not deferred: the window path hands it to uninstallWindow, and the quiet path
	// releases before RemoveAfterExit, its last step
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=bbf95339416de217213cc6b478507cc780ed38aa63d1af46d11e5a548b3d3bb4 ====
