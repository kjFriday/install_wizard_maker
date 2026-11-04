// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/wizard/setup.go
// About: Package wizard is what a built setup.exe and the uninstall.exe it leaves behind run.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package wizard is what a built setup.exe and the uninstall.exe it leaves behind run.
//
// It holds their command lines, their windows, and the steps both share. main opens its own exe's
// payload and, by its kind, calls RunSetup (this file) or RunUninstall (uninstall.go). The window
// side is in the *_windows.go files: frame_windows.go (the shared chrome), setup_windows.go and
// uninstall_windows.go; flow.go is their page order, free of windows.
//
// The main flow of a setup.exe (RunSetup):
//  1. Parse the flags (parseSetup); /S counts as -quiet.
//  2. openSetup reads setup.json and app.ico from the payload, picks the real places and HKCU (or a
//     -root sandbox with a registry.json), and finds any earlier install to upgrade or repair. It
//     refuses to run elevated and refuses to downgrade.
//  3. -version prints what this setup installs and stops.
//  4. Take the per-program lock (lockName) that the uninstaller also takes.
//  5. Without -quiet, hand over to the window (setupWindow); with it, quietSetup checks and installs.
//
// Types: setupOptions (the parsed flags), Setup (one run), uninstallInfo (uninstall.json).
//
// Start reading at RunSetup.
package wizard

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/install"
	"generic-company/wizard/internal/payload"
	"generic-company/wizard/internal/reg"
	"generic-company/wizard/internal/shell"
	"generic-company/wizard/internal/spec"
	"generic-company/wizard/internal/sys"
)

// setupOptions are a setup.exe's flags, as parseSetup reads them; setupUsage describes each.
type setupOptions struct {
	quiet, desktop, noStartMenu, version bool
	dir, root, log, smoke                string
}

// parseSetup reads a setup.exe's arguments (after the exe name) into setupOptions.
//
// The flag set neither prints nor exits: errors come back for RunSetup to show with setupUsage. /S
// is first rewritten to -quiet (aliases), and any argument left over after the flags is an error.
func parseSetup(args []string) (setupOptions, error) {
	var o setupOptions
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.quiet, "quiet", false, "no window: install with the defaults (accepts the license)")
	fs.StringVar(&o.dir, "dir", "", "install folder (a new or empty one)")
	fs.BoolVar(&o.desktop, "desktop-shortcut", false, "add a desktop shortcut")
	fs.BoolVar(&o.noStartMenu, "no-start-menu", false, "no Start-menu shortcut")
	fs.StringVar(&o.root, "root", "", "sandbox: install under this folder (registry goes to registry.json)")
	fs.StringVar(&o.log, "log", "", "append what happens to this file")
	fs.StringVar(&o.smoke, "smoke", "", "walk the window's pages, write a report to this file, close")
	fs.BoolVar(&o.version, "version", false, "print what this setup installs")
	err := fs.Parse(aliases(args))
	if err == nil && fs.NArg() > 0 {
		err = fmt.Errorf("unexpected %q", fs.Arg(0))
	}
	return o, err
}

// aliases accepts /S for -quiet, as other installers do.
func aliases(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.EqualFold(a, "/S") {
			a = "-quiet"
		}
		out[i] = a
	}
	return out
}

// setupUsage is shown after a flag error. It leaves out -smoke (for the tests) and -version.
const setupUsage = `Usage: setup.exe [-quiet] [-dir PATH] [-desktop-shortcut] [-no-start-menu] [-root DIR] [-log FILE]
  -quiet (or /S)      no window: install with the defaults; this accepts the license
  -dir PATH           install folder (a new or empty one)
  -desktop-shortcut   also add a desktop shortcut
  -no-start-menu      no Start-menu shortcut
  -root DIR           sandbox: everything under DIR, the registry in DIR\registry.json
  -log FILE           append what happens to FILE`

// Setup is one run of a setup.exe: what it installs, from where, to where.
//
// openSetup makes one; the window or quietSetup then calls Check and Install.
type Setup struct {
	M        *spec.Manifest   // setup.json from the payload
	P        *payload.File    // this exe's payload: the program's files
	Places   install.Places   // the folders to use (the user's, or a -root sandbox's)
	Registry install.Registry // HKCU, or a sandbox's registry.json
	Old      *install.Marker  // the install this one upgrades or repairs
	Choices  install.Choices  // folder and shortcuts; the defaults until the user changes them
	Icon     []byte           // app.ico from the payload
	Python   string           // pythonw.exe for a Python program, filled in by Check
}

// Upgrade reports whether an earlier install of the program is there.
func (s *Setup) Upgrade() bool { return s.Old != nil }

// openSetup reads the payload and looks at the computer: places, registry, any earlier install, defaults.
//
// It may return a Setup together with an error, so the caller can still title its messages with the
// program's name. Refusals are *install.Refusal; a bad setup.json is payload.ErrDamaged.
//
// Steps:
//  1. Read setup.json (at most 16 MiB) and app.ico (at most 4 MiB) from the payload.
//  2. With -root, everything goes into that sandbox folder and its registry.json; elevation is not
//     checked there. Otherwise refuse to run as administrator (setup installs per user) and use the
//     user's folders and HKCU.
//  3. Look for an earlier install. An upgrade keeps its folder and its shortcuts as they are; a
//     marker with no shortcuts and no installer comes from another installer, so the manifest's
//     defaults apply. A new install takes -dir, if given.
//  4. Apply -desktop-shortcut and -no-start-menu over that.
//  5. Refuse a setup older than the installed version.
func openSetup(p *payload.File, o setupOptions) (*Setup, error) {
	b, err := p.ReadAll("setup.json", 16<<20)
	if err != nil {
		return nil, err
	}
	m, err := spec.ParseManifest(b)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", payload.ErrDamaged, err)
	}
	icon, err := p.ReadAll("app.ico", 4<<20)
	if err != nil {
		return nil, err
	}
	s := &Setup{M: m, P: p, Icon: icon}
	if o.root != "" {
		s.Places = install.SandboxPlaces(o.root)
		s.Registry = reg.File{Path: filepath.Join(s.Places.Root, "registry.json")}
	} else {
		if sys.Elevated() {
			return s, &install.Refusal{Msg: "Setup installs for you alone and needs no administrator rights. " +
				"Run it again without \"Run as administrator\"."}
		}
		if s.Places, err = shell.UserPlaces(); err != nil {
			return s, err
		}
		s.Registry = reg.HKCU{}
	}
	dir, old := install.Existing(s.Registry, s.Places, m.ID, m.Group, m.Name)
	s.Old = old
	s.Choices = install.Choices{Dir: s.Places.Dir(m.Group, m.Name), StartMenu: m.Shortcuts.StartMenu, Desktop: m.Shortcuts.Desktop}
	if old != nil {
		s.Choices.Dir = dir
		s.Choices.StartMenu, s.Choices.Desktop = false, false
		for _, l := range old.Shortcuts {
			s.Choices.StartMenu = s.Choices.StartMenu || strings.EqualFold(l, s.Places.StartMenuLink(m.Group, m.Name))
			s.Choices.Desktop = s.Choices.Desktop || strings.EqualFold(l, s.Places.DesktopLink(m.Name))
		}
		if len(old.Shortcuts) == 0 && old.Installer == "" { // another installer's marker: use the defaults
			s.Choices.StartMenu, s.Choices.Desktop = m.Shortcuts.StartMenu, m.Shortcuts.Desktop
		}
	} else if o.dir != "" {
		if s.Choices.Dir, err = filepath.Abs(o.dir); err != nil {
			return s, err
		}
	}
	if o.desktop {
		s.Choices.Desktop = true
	}
	if o.noStartMenu {
		s.Choices.StartMenu = false
	}
	if old != nil && spec.CompareVersions(m.Version, old.Version) < 0 {
		return s, &install.Refusal{Msg: fmt.Sprintf("%s %s is already installed; this setup is the older %s.", m.Name, old.Version, m.Version)}
	}
	return s, nil
}

// Check is everything to settle before installing: the folder, and Python when the program needs it.
//
// The folder is checked only for a new install (an upgrade reuses its own). For a Python program it
// finds pythonw.exe once and keeps it in s.Python. Problems the user can fix are *install.Refusal.
func (s *Setup) Check() error {
	if s.Old == nil {
		if err := install.CheckFolder(s.Choices.Dir, s.M.ID, s.Places); err != nil {
			return err
		}
	}
	if s.M.Run.Program == spec.Python && s.Python == "" {
		s.Python = findPython(s.M.Requires.Python)
		if s.Python == "" {
			return &install.Refusal{Msg: fmt.Sprintf("%s needs Python %s or newer, which is not installed. "+
				"Get it from python.org, then run setup again.", s.M.Name, s.M.Requires.Python)}
		}
	}
	return nil
}

// Install does it. progress and cancelled may be nil.
//
// progress gets the bytes done so far, the total and the current file; cancelled is polled as files
// are unpacked, and true puts everything back and returns install.ErrCancelled. Both are called on
// the goroutine that runs Install. Call Check first.
//
// Steps:
//  1. Build uninstall.exe: this exe's own stub with a small payload naming the program.
//  2. Plan the install (install.MakePlan) from the manifest, places, choices and any old install.
//  3. Refuse, with Retry, when the drive has less free space than the files, the uninstaller and a
//     16 MiB margin.
//  4. Apply the plan as one transaction: it completes, or leaves everything as it was.
func (s *Setup) Install(progress func(done, total int64, file string), cancelled func() bool) (install.Result, error) {
	s.Choices.Python = s.Python
	stub, err := s.P.StubBytes()
	if err != nil {
		return install.Result{}, err
	}
	uninstaller, err := makeUninstaller(stub, s.M)
	if err != nil {
		return install.Result{}, err
	}
	plan, err := install.MakePlan(s.M, s.Places, s.Choices, s.Old, time.Now(), int64(len(uninstaller)))
	if err != nil {
		return install.Result{}, err
	}
	need := uint64(s.M.Size()) + uint64(len(uninstaller)) + 16<<20
	if free, err := sys.FreeSpace(plan.Dir); err == nil && free < need {
		return install.Result{}, &install.Refusal{Retry: true, Msg: fmt.Sprintf("Setup needs %d MB on that drive and %d MB are free.", need>>20, free>>20)}
	}
	return install.Apply(plan, install.Env{Places: s.Places, Registry: s.Registry, Link: shell.MakeLinkDir,
		Source: s.P, Icon: s.Icon, Uninstaller: uninstaller, Progress: progress, Cancelled: cancelled})
}

// LaunchTarget is what Launch on the finish page starts: the new Start-menu (or desktop) shortcut, so
// the program gets its own taskbar identity and working folder, else the program itself.
func (s *Setup) LaunchTarget() (file, args, dir string) {
	if s.Choices.StartMenu {
		return s.Places.StartMenuLink(s.M.Group, s.M.Name), "", ""
	}
	if s.Choices.Desktop {
		return s.Places.DesktopLink(s.M.Name), "", ""
	}
	program := filepath.Join(s.Choices.Dir, filepath.FromSlash(s.M.Run.Program))
	if s.M.Run.Program == spec.Python {
		program = s.Python
	}
	vars := spec.ArgVars(s.Choices.Dir, s.M.Name, s.M.Version, s.Places.LocalAppData, s.Places.AppData)
	var quoted []string
	for _, a := range s.M.Run.Args {
		x, _ := spec.Expand(a, vars)
		quoted = append(quoted, quoteForShell(x))
	}
	return program, strings.Join(quoted, " "), s.Choices.Dir
}

// quoteForShell quotes one argument for a command line: an empty one, or one with a space, tab or
// quote, goes in double quotes with each inner quote as \".
//
// Known limitation: backslashes are left alone. By the usual Windows rules (CommandLineToArgvW and the
// C runtime) backslashes just before a quote escape it, so an argument ending in a backslash, such as
// a folder "C:\My Apps\", reaches the program with the closing quote taken as text.
func quoteForShell(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// human is a size for people.
//
// Below 1 MiB it is whole KB rounded up (so 1 byte is "1 KB"); below 1 GiB, MB with one decimal; then
// GB. The units are binary (1 KB = 1024 bytes).
func human(n int64) string {
	switch {
	case n < 1024*1024:
		return fmt.Sprintf("%d KB", (n+1023)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
}

// uninstallInfo is uninstall.json, the payload of an uninstall.exe: which program it may remove.
type uninstallInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Maker   string `json:"maker"`
}

// makeUninstaller is the uninstall.exe for m: stub (this exe's program part, see StubBytes) and an
// Uninstall payload holding only uninstall.json. The same program thus runs as the uninstaller.
func makeUninstaller(stub []byte, m *spec.Manifest) ([]byte, error) {
	info, _ := json.MarshalIndent(uninstallInfo{ID: m.ID, Name: m.Name, Version: m.Version, Maker: m.Maker}, "", "  ")
	var b bytes.Buffer
	err := payload.Write(&b, stub, payload.Uninstall, []payload.Entry{payload.Bytes("uninstall.json", info)})
	return b.Bytes(), err
}

// exitCode maps an outcome to the exit codes every role shares.
func exitCode(err error) int {
	var r *install.Refusal
	switch {
	case err == nil:
		return app.ExitOK
	case errors.As(err, &r):
		return app.ExitRefused
	case errors.Is(err, install.ErrCancelled):
		return app.ExitCancelled
	}
	return app.ExitFailed
}

// lockName is the session mutex that keeps a program's setup and its uninstaller from running at the
// same time. Setups and uninstallers made by every version of the maker must agree on it: never change it.
func lockName(id string) string { return "GenericCompany.Wizard." + id }

// RunSetup is a setup.exe's main.
//
// self is the exe's path, p its payload, args the arguments after the exe name. It returns the exit
// code (app.Exit*). Its own messages go through sys.Output: to the console when there is one, else one
// message box at the end (none with -quiet), and to the -log file too. See the package comment for
// the steps.
func RunSetup(self string, p *payload.File, args []string) int {
	o, err := parseSetup(args)
	out, logErr := sys.NewOutput("Setup", o.quiet, o.log)
	if err != nil {
		out.Printf("%v\n\n%s", err, setupUsage)
		out.Done(sys.Error)
		return app.ExitUsage
	}
	if logErr != nil {
		out.Printf("log: %v", logErr)
	}
	s, err := openSetup(p, o)
	if s != nil && s.M != nil {
		out.SetTitle(s.M.Name + " " + s.M.Version + " Setup")
	}
	if err != nil {
		out.Printf("%v", err)
		out.Done(sys.Error)
		return exitCode(err)
	}
	if o.version {
		out.Printf("%s %s, by %s (made with %s)", s.M.Name, s.M.Version, s.M.Publisher, s.M.Maker)
		out.Done(sys.Info)
		return app.ExitOK
	}
	release, ok := sys.Lock(lockName(s.M.ID))
	if !ok {
		out.Printf("Setup or uninstall of %s is already running.", s.M.Name)
		out.Done(sys.Warning)
		return app.ExitRefused
	}
	defer release()
	if !o.quiet {
		return setupWindow(s, o, out)
	}
	return quietSetup(s, out)
}

// quietSetup installs with no window (-quiet or /S): Check, Install, then report the result and the
// files kept because they were changed since installed or were not installed by this setup.
func quietSetup(s *Setup, out *sys.Output) int {
	if err := s.Check(); err != nil {
		out.Printf("%v", err)
		out.Done(sys.Error)
		return exitCode(err)
	}
	verb := "Installing"
	if s.Upgrade() {
		verb = "Upgrading"
	}
	out.Printf("%s %s %s in %s", verb, s.M.Name, s.M.Version, s.Choices.Dir)
	res, err := s.Install(nil, nil)
	if err != nil {
		out.Printf("%v", err)
		out.Done(sys.Error)
		return exitCode(err)
	}
	out.Printf("Installed %s %s.", s.M.Name, s.M.Version)
	for _, k := range res.Kept {
		out.Printf("Kept (changed since installed, or not installed by this setup): %s", k)
	}
	out.Done(sys.Info)
	return app.ExitOK
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=19dcf1921c2188b0a118dc1b637185b7738b90469a9598a4e1fee8a9de7d5cae ====
