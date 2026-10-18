// Package wizard is what a built setup.exe runs (the install wizard) and the uninstall.exe it leaves
// behind: their command lines, their windows, and the steps both share.
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

	"acme/wizard/internal/app"
	"acme/wizard/internal/install"
	"acme/wizard/internal/payload"
	"acme/wizard/internal/reg"
	"acme/wizard/internal/shell"
	"acme/wizard/internal/spec"
	"acme/wizard/internal/sys"
)

type setupOptions struct {
	quiet, desktop, noStartMenu, version bool
	dir, root, log, smoke                string
}

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

const setupUsage = `Usage: setup.exe [-quiet] [-dir PATH] [-desktop-shortcut] [-no-start-menu] [-root DIR] [-log FILE]
  -quiet (or /S)      no window: install with the defaults; this accepts the license
  -dir PATH           install folder (a new or empty one)
  -desktop-shortcut   also add a desktop shortcut
  -no-start-menu      no Start-menu shortcut
  -root DIR           sandbox: everything under DIR, the registry in DIR\registry.json
  -log FILE           append what happens to FILE`

// Setup is one run of a setup.exe: what it installs, from where, to where.
type Setup struct {
	M        *spec.Manifest
	P        *payload.File
	Places   install.Places
	Registry install.Registry
	Old      *install.Marker // the install this one upgrades or repairs
	Choices  install.Choices
	Icon     []byte
	Python   string
}

// Upgrade reports whether an earlier install of the program is there.
func (s *Setup) Upgrade() bool { return s.Old != nil }

// openSetup reads the payload and looks at the computer: places, registry, any earlier install, defaults.
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

// uninstallInfo is uninstall.json, the payload of an uninstall.exe: which program it may remove.
type uninstallInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Maker   string `json:"maker"`
}

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

// RunSetup is a setup.exe's main.
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
	release, ok := sys.Lock("ACME.Wizard." + s.M.ID)
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
