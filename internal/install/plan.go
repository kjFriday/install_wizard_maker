// Package install puts a setup's files in place for the current user, and takes them away again.
//
// Install is one transaction. Files are unpacked and checked in <dir>\.setup\new; files they replace
// move to <dir>\.setup\old; only when every step (files, uninstaller, marker, shortcuts, registry) has
// worked is .setup deleted, and any failure puts back exactly what was there. Uninstall removes only
// what the marker lists, and only files that are still as they were installed.
//
// The marker (install.json) keeps the fields the other ACME programs' self-installers write, so each
// can take over or remove the other's install.
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"acme/wizard/internal/spec"
)

// Names setup writes into every install folder.
const (
	MarkerName      = "install.json"
	UninstallerName = "uninstall.exe"
	IconName        = "app.ico"
	setupDir        = ".setup"
	uninstallKey    = `Software\Microsoft\Windows\CurrentVersion\Uninstall`
)

// Places are the folders an install touches.
type Places struct {
	Programs     string // per-user programs folder (%LOCALAPPDATA%\Programs)
	StartMenu    string // the Start menu's Programs folder
	Desktop      string
	LocalAppData string // for purge entries
	AppData      string
	Root         string // the -root sandbox; "" on the real computer
}

// SandboxPlaces is the -root layout the other ACME programs use: everything under root, and the
// registry in root\registry.json.
func SandboxPlaces(root string) Places {
	root, _ = filepath.Abs(root)
	return Places{Programs: filepath.Join(root, "Programs"), StartMenu: filepath.Join(root, "Start Menu"),
		Desktop: filepath.Join(root, "Desktop"), LocalAppData: filepath.Join(root, "LocalAppData"),
		AppData: filepath.Join(root, "AppData"), Root: root}
}

// Dir is the default install folder: Programs\[group\]name.
func (p Places) Dir(group, name string) string { return filepath.Join(p.Programs, group, name) }

// StartMenuLink is the Start-menu shortcut: [group\]name.lnk.
func (p Places) StartMenuLink(group, name string) string {
	return filepath.Join(p.StartMenu, group, name+".lnk")
}

// DesktopLink is the desktop shortcut.
func (p Places) DesktopLink(name string) string { return filepath.Join(p.Desktop, name+".lnk") }

// ownsLink: shortcuts are only ever .lnk files in the Start menu or on the desktop.
func (p Places) ownsLink(path string) bool {
	if !strings.EqualFold(filepath.Ext(path), ".lnk") {
		return false
	}
	return within(p.StartMenu, path) || within(p.Desktop, path)
}

// within reports whether path is strictly inside dir.
func within(dir, path string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// Marker is install.json.
type Marker struct {
	AppID     string            `json:"app_id"`
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Files     []string          `json:"files"`
	Shortcuts []string          `json:"shortcuts"`
	SHA256    map[string]string `json:"sha256,omitempty"`
	Purge     []string          `json:"purge,omitempty"`
	Installer string            `json:"installer,omitempty"`
}

// ReadMarker reads dir's install.json; nil when there is none.
func ReadMarker(dir string) (*Marker, error) {
	b, err := os.ReadFile(filepath.Join(dir, MarkerName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Marker
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, MarkerName), err)
	}
	return &m, nil
}

// JSON is the marker as written.
func (m *Marker) JSON() []byte {
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}

// entry normalizes a marker path: the Go programs write bare names, the Python ones slash paths, the
// Rust one folder names. Anything that is not a plain relative path is ignored.
func entry(p string) (string, bool) {
	p = strings.Trim(strings.ReplaceAll(p, `\`, "/"), "/")
	p = strings.TrimPrefix(p, "./")
	if p == "" || !filepath.IsLocal(filepath.FromSlash(p)) {
		return "", false
	}
	return p, true
}

// Refusal is a reason not to go on that is nobody's fault: exit code 4, and nothing was changed.
type Refusal struct {
	Msg   string
	Retry bool // the user can put it right (close a program, move a file) and try again
}

func (r *Refusal) Error() string { return r.Msg }

func refuse(retry bool, format string, a ...any) error {
	return &Refusal{Msg: fmt.Sprintf(format, a...), Retry: retry}
}

// Choices are what the user picked (or the flags said).
type Choices struct {
	Dir       string
	StartMenu bool
	Desktop   bool
	Python    string // pythonw.exe, when run.program is {python}
}

// Shortcut is one .lnk file.
type Shortcut struct {
	Path, Target, Args, WorkDir, Icon, Description, AppUserModelID string
}

// Plan is everything an install will do, as plain data.
type Plan struct {
	M        *spec.Manifest
	Dir      string
	Old      *Marker  // the install being upgraded or repaired; nil for a new one
	Remove   []string // files the old version had and this one drops (removed if unchanged)
	Kept     []string // files the old version had, this one drops, and setup cannot vouch for (no hash)
	Links    []Shortcut
	Unlink   []string // the old version's shortcuts that are no longer wanted
	Registry []Value
	Marker   Marker
}

// Program is what the shortcuts start: the packed program, or the Python the caller found.
func (p *Plan) Program(python string) string {
	if p.M.Run.Program == spec.Python {
		return python
	}
	return filepath.Join(p.Dir, filepath.FromSlash(p.M.Run.Program))
}

// MakePlan works out an install of m into c.Dir, over old (the marker found there, or nil).
// uninstaller is the size of uninstall.exe, for the size Apps & features shows.
func MakePlan(m *spec.Manifest, pl Places, c Choices, old *Marker, today time.Time, uninstaller int64) (*Plan, error) {
	dir, err := filepath.Abs(c.Dir)
	if err != nil {
		return nil, err
	}
	if old != nil {
		if old.AppID != m.ID {
			return nil, refuse(false, "%s belongs to %s", dir, old.AppID)
		}
		if spec.CompareVersions(m.Version, old.Version) < 0 {
			return nil, refuse(false, "%s %s is already installed; this setup is the older %s", m.Name, old.Version, m.Version)
		}
	}
	if m.Run.Program == spec.Python && c.Python == "" {
		return nil, refuse(false, "%s needs Python %s or newer, and none was found (python.org)", m.Name, m.Requires.Python)
	}
	p := &Plan{M: m, Dir: dir, Old: old}

	shipped := map[string]bool{strings.ToLower(IconName): true, strings.ToLower(UninstallerName): true}
	for _, f := range m.Files {
		shipped[strings.ToLower(f.Path)] = true
	}
	if old != nil {
		for _, raw := range old.Files {
			e, ok := entry(raw)
			if !ok || shipped[strings.ToLower(e)] || strings.EqualFold(e, MarkerName) {
				continue
			}
			if old.SHA256[raw] != "" || old.SHA256[e] != "" {
				p.Remove = append(p.Remove, e)
			} else {
				p.Kept = append(p.Kept, e)
			}
		}
	}

	args := make([]string, 0, len(m.Run.Args))
	vars := spec.ArgVars(dir, m.Name, m.Version, pl.LocalAppData, pl.AppData)
	for _, a := range m.Run.Args {
		x, err := spec.Expand(a, vars)
		if err != nil {
			return nil, err
		}
		args = append(args, syscall.EscapeArg(x))
	}
	link := Shortcut{Target: p.Program(c.Python), Args: strings.Join(args, " "), WorkDir: dir,
		Icon: filepath.Join(dir, IconName), Description: m.Name + " " + m.Version, AppUserModelID: m.Run.AppUserModelID}
	if c.StartMenu {
		link.Path = pl.StartMenuLink(m.Group, m.Name)
		p.Links = append(p.Links, link)
	}
	if c.Desktop {
		link.Path = pl.DesktopLink(m.Name)
		p.Links = append(p.Links, link)
	}
	if old != nil {
		for _, s := range old.Shortcuts {
			if pl.ownsLink(s) && !p.links(s) {
				p.Unlink = append(p.Unlink, s)
			}
		}
	}

	size := m.Size() + uninstaller
	uninstall := quoteArg(filepath.Join(dir, UninstallerName))
	if pl.Root != "" {
		uninstall += " -root " + quoteArg(pl.Root)
	}
	p.Registry = []Value{
		Str("DisplayName", m.Name+" "+m.Version),
		Str("DisplayVersion", m.Version),
		Str("Publisher", m.Publisher),
		Str("DisplayIcon", filepath.Join(dir, IconName)),
		Str("InstallLocation", dir),
		Str("UninstallString", uninstall),
		Str("QuietUninstallString", uninstall+" -quiet"),
		Str("InstallDate", today.Format("20060102")),
		DWord("NoModify", 1),
		DWord("NoRepair", 1),
		DWord("EstimatedSize", uint32(size/1024)+1),
	}

	p.Marker = Marker{AppID: m.ID, Name: m.Name, Version: m.Version, SHA256: map[string]string{},
		Purge: m.Purge, Installer: m.Maker}
	for _, f := range m.Files {
		p.Marker.Files = append(p.Marker.Files, f.Path)
		p.Marker.SHA256[f.Path] = f.SHA256
	}
	p.Marker.Files = append(p.Marker.Files, IconName, UninstallerName)
	for _, l := range p.Links {
		p.Marker.Shortcuts = append(p.Marker.Shortcuts, l.Path)
	}
	if p.Marker.Shortcuts == nil {
		p.Marker.Shortcuts = []string{}
	}
	sort.Strings(p.Remove)
	sort.Strings(p.Kept)
	return p, nil
}

func (p *Plan) links(path string) bool {
	for _, l := range p.Links {
		if strings.EqualFold(l.Path, path) {
			return true
		}
	}
	return false
}

// quoteArg always quotes a path for a command line (paths cannot hold a double quote; trailing
// backslashes are doubled so they do not escape the closing quote).
func quoteArg(s string) string {
	n := len(s) - len(strings.TrimRight(s, `\`))
	return `"` + s + strings.Repeat(`\`, n) + `"`
}

// CheckFolder: setup installs only into a new or empty folder, or the folder of an earlier install of
// the same program, so it can never overwrite (or later remove) a file of the user's.
func CheckFolder(dir, id string, pl Places) error {
	if !filepath.IsAbs(dir) || filepath.VolumeName(dir) == "" {
		return refuse(true, "Choose a full path such as %s", pl.Dir("", "Example"))
	}
	clean := filepath.Clean(dir)
	if len(clean) > 150 {
		return refuse(true, "That folder's path is too long (more than 150 characters).")
	}
	if clean == filepath.VolumeName(clean)+`\` {
		return refuse(true, "Choose a folder, not the whole drive.")
	}
	home, _ := os.UserHomeDir()
	for _, known := range []string{pl.Programs, pl.StartMenu, pl.Desktop, pl.LocalAppData, pl.AppData, pl.Root, home,
		os.Getenv("SystemRoot"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if known != "" && strings.EqualFold(clean, filepath.Clean(known)) {
			return refuse(true, "%s is a system folder; choose a folder inside it.", clean)
		}
	}
	info, err := os.Stat(clean)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return refuse(true, "%s is a file.", clean)
	}
	m, err := ReadMarker(clean)
	if err != nil {
		return refuse(true, "%s holds an unreadable %s.", clean, MarkerName)
	}
	if m != nil {
		if m.AppID != id {
			return refuse(true, "%s belongs to %s; choose another folder.", clean, m.AppID)
		}
		return nil
	}
	if entries, _ := os.ReadDir(clean); len(entries) > 0 {
		return refuse(true, "%s is not empty; choose a new or empty folder.", clean)
	}
	return nil
}

// Existing is an earlier install of the program: from the Uninstall key's InstallLocation, or the
// default folder, and only if the marker there names the same program.
func Existing(reg Registry, pl Places, id, group, name string) (dir string, m *Marker) {
	var tries []string
	if vals, found, err := reg.Read(id); err == nil && found {
		if v, ok := Lookup(vals, "InstallLocation"); ok && v.Text != "" {
			tries = append(tries, v.Text)
		}
	}
	tries = append(tries, pl.Dir(group, name))
	for _, d := range tries {
		if m, err := ReadMarker(d); err == nil && m != nil && m.AppID == id {
			return d, m
		}
	}
	return "", nil
}
