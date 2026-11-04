// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/install/plan.go
// About: Package install puts a setup's files in place for the current user and removes them again.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package install puts a setup's files in place for the current user and removes them again.
//
// Install is one transaction. Files are unpacked and checked in <dir>\.setup\new; files they replace
// move to <dir>\.setup\old; only when every step (files, uninstaller, marker, shortcuts, registry) has
// worked is .setup deleted, and any failure puts back exactly what was there. Uninstall removes only
// what the marker lists, and only files that are still as they were installed.
//
// The marker (install.json) keeps the fields the other Generic Company programs' self-installers write, so each
// can take over or remove the other's install.
//
// The wizard (package wizard) and the maker's own -install/-uninstall drive it in this order:
//  1. Existing finds an earlier install; CheckFolder vets the folder the user picked.
//  2. MakePlan turns the manifest, the folders and the user's choices into a Plan: pure data, nothing
//     on disk changes.
//  3. Apply (apply.go) carries the Plan out as the transaction above.
//  4. Later, the uninstaller reads the marker with ReadMarker and calls Uninstall (uninstall.go), then
//     DeleteAfterExit to remove its own exe.
//
// Files: plan.go holds the marker, the plan and the folder checks; places.go the folder layout;
// registry.go the Apps & features values; apply.go the transaction; uninstall.go the removal.
// Start reading at MakePlan, then Apply.
//
// Every path in a marker is treated as untrusted data (another program, or a person, may have written
// it): entry drops anything that is not a plain relative path, and Places.ownsLink and within keep
// shortcuts and folders to the places this package owns.
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

	"generic-company/wizard/internal/spec"
)

// Names setup writes into every install folder, and the work folder it uses while installing.
const (
	MarkerName      = "install.json"  // the marker: what this install put where
	UninstallerName = "uninstall.exe" // the uninstaller, which UninstallString in Apps & features runs
	IconName        = "app.ico"       // the program's icon, for the shortcuts and Apps & features
	setupDir        = ".setup"        // Apply's staging folder (new\ and old\); its removal is the commit
)

// Marker is install.json. The JSON keys and the field order are shared with the other Generic
// Company programs' self-installers (Go, Python and Rust), which read and write the same file, so
// renaming a tag or reordering the fields changes a format other programs depend on.
//
// The last three fields are this package's additions, which the siblings ignore:
//   - SHA256 maps a Files entry to its hex SHA-256 as installed. A file an upgrade drops, or an
//     uninstall reaches, is removed only while its hash still matches, and an entry without a hash
//     is never removed. (A file the new version ships again is replaced whatever its hash.)
//   - Purge lists the settings folders (with {localappdata}/{appdata} placeholders) that an
//     uninstall with "also remove settings" deletes.
//   - Installer names the maker and version that built the setup.
type Marker struct {
	AppID     string            `json:"app_id"`    // the program's id; a folder belongs to the program it names
	Name      string            `json:"name"`      // display name
	Version   string            `json:"version"`   // dotted numeric, compared with spec.CompareVersions
	Files     []string          `json:"files"`     // installed paths relative to the folder ("/" or "\"; may be folders)
	Shortcuts []string          `json:"shortcuts"` // absolute .lnk paths; written as [] rather than null
	SHA256    map[string]string `json:"sha256,omitempty"`
	Purge     []string          `json:"purge,omitempty"`
	Installer string            `json:"installer,omitempty"`
}

// ReadMarker reads dir's install.json; nil when there is none.
// A marker that exists but is not valid JSON is an error naming the file; the fields are not checked
// here, so callers compare AppID themselves.
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

// JSON is the marker as written: two-space indented, in field order, with a final newline. Marshal
// cannot fail for these field types, so its error is dropped.
func (m *Marker) JSON() []byte {
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}

// entry normalizes a marker path: the Go programs write bare names, the Python ones slash paths, the
// Rust one folder names. Anything that is not a plain relative path is ignored.
// The result uses "/" with no leading "./" or surrounding slashes (so a rooted "\x" reads as "x",
// inside the folder). ok is false for "", paths with a drive ("C:\..." or "C:x"), reserved device
// names such as NUL, and anything that climbs out with "..".
func entry(p string) (string, bool) {
	p = strings.Trim(strings.ReplaceAll(p, `\`, "/"), "/")
	p = strings.TrimPrefix(p, "./")
	if p == "" || !filepath.IsLocal(filepath.FromSlash(p)) {
		return "", false
	}
	return p, true
}

// Refusal is a reason not to go on that is nobody's fault: exit code 4, and nothing was changed.
// Callers find it with errors.As. The wizard shows Msg as written, so it is a full sentence that says
// what to do; the tests match some of these texts.
type Refusal struct {
	Msg   string
	Retry bool // the user can put it right (close a program, move a file) and try again
}

// Error returns the message as written, with no prefix.
func (r *Refusal) Error() string { return r.Msg }

// refuse makes a *Refusal from a fmt format, as an error.
func refuse(retry bool, format string, a ...any) error {
	return &Refusal{Msg: fmt.Sprintf(format, a...), Retry: retry}
}

// Choices are what the user picked (or the flags said).
type Choices struct {
	Dir       string // install folder; made absolute by MakePlan
	StartMenu bool   // add the Start-menu shortcut
	Desktop   bool   // add the desktop shortcut
	Python    string // pythonw.exe, when run.program is {python}
}

// Shortcut is one .lnk file: what Env.Link writes (shell.MakeLinkDir on a real install).
// Path is the .lnk file itself; Target, Args and WorkDir are what it starts; Icon is the .ico it shows;
// Description is its tooltip; AppUserModelID, when set, groups the program's windows under the
// shortcut on the taskbar.
type Shortcut struct {
	Path, Target, Args, WorkDir, Icon, Description, AppUserModelID string
}

// Plan is everything an install will do, as plain data. MakePlan builds it and Apply carries it out;
// the wizard also reads Dir. Paths in Remove and Kept are folder-relative with "/".
type Plan struct {
	M        *spec.Manifest // what the setup packs
	Dir      string         // absolute install folder
	Old      *Marker        // the install being upgraded or repaired; nil for a new one
	Remove   []string       // files the old version had and this one drops (removed if unchanged)
	Kept     []string       // files the old version had, this one drops, and setup cannot vouch for (no hash)
	Links    []Shortcut     // shortcuts to write, Start menu first
	Unlink   []string       // the old version's shortcuts that are no longer wanted
	Registry []Value        // the Apps & features values, written as the key's whole contents
	Marker   Marker         // the new install.json; Apply adds the icon's and uninstaller's hashes
}

// Program is what the shortcuts start: the packed program, or the Python the caller found.
// python is the pythonw.exe path from Choices; it is used only when run.program is {python}.
func (p *Plan) Program(python string) string {
	if p.M.Run.Program == spec.Python {
		return python
	}
	return filepath.Join(p.Dir, filepath.FromSlash(p.M.Run.Program))
}

// MakePlan works out an install of m into c.Dir, over old (the marker found there, or nil).
// uninstaller is the size of uninstall.exe, for the size Apps & features shows.
// today is the install date Apps & features shows (a parameter so tests get a fixed one).
// It reads nothing from disk and changes nothing: the result is plain data for Apply.
//
// It returns a *Refusal when old belongs to another program, when old is a newer version (no
// downgrades), or when the program needs Python and c.Python is empty. The wizard checks these
// first too; these are the safety net for every other caller. An error from spec.Expand means
// run.args holds a bad placeholder.
//
// Steps:
//  1. Refuse as above.
//  2. Sort the old marker's files that this version no longer ships into Remove (a hash proves
//     setup put them there) and Kept (no hash: another installer's, so left alone).
//  3. Build the shortcuts: one Shortcut value, copied for the Start menu and the desktop, and
//     Unlink for the old shortcuts the user no longer wants.
//  4. Build the Apps & features values.
//  5. Build the new marker: the packed files with their hashes, then app.ico and uninstall.exe.
func MakePlan(m *spec.Manifest, pl Places, c Choices, old *Marker, today time.Time, uninstaller int64) (*Plan, error) {
	dir, err := filepath.Abs(c.Dir)
	if err != nil {
		return nil, err
	}
	// 1. refusals: nothing has been worked out yet
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

	// 2. what the old version had and this one drops. Names compare case-insensitively, as Windows
	// file names do; the marker itself is always rewritten, never dropped.
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
			// the hash may be keyed by the raw entry or by its normalized form
			if old.SHA256[raw] != "" || old.SHA256[e] != "" {
				p.Remove = append(p.Remove, e)
			} else {
				p.Kept = append(p.Kept, e)
			}
		}
	}

	// 3. shortcuts. Each argument is expanded ({app}, {localappdata}, ...) and then quoted by
	// syscall.EscapeArg, the inverse of how the started program's C runtime splits its command line.
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
	// link is a struct value: append copies it, so setting Path again for the desktop leaves the
	// Start-menu entry as it was
	if c.StartMenu {
		link.Path = pl.StartMenuLink(m.Group, m.Name)
		p.Links = append(p.Links, link)
	}
	if c.Desktop {
		link.Path = pl.DesktopLink(m.Name)
		p.Links = append(p.Links, link)
	}
	if old != nil {
		// an old shortcut outside the Start menu and desktop is never touched, whatever the marker says
		for _, s := range old.Shortcuts {
			if pl.ownsLink(s) && !p.links(s) {
				p.Unlink = append(p.Unlink, s)
			}
		}
	}

	// 4. the Apps & features entry. UninstallString is what Windows runs for "Uninstall";
	// QuietUninstallString is the documented key for an unattended uninstall. In a sandbox both carry
	// -root, so the uninstaller finds the sandbox and its registry.json.
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
		Str("InstallDate", today.Format("20060102")), // YYYYMMDD, the format Apps & features expects
		DWord("NoModify", 1),                         // no Modify button: setup has no change mode
		DWord("NoRepair", 1),                         // no Repair button: running the setup again repairs
		DWord("EstimatedSize", uint32(size/1024)+1),  // in KiB; the +1 rounds up so a small program never shows 0
	}

	// 5. the new marker
	p.Marker = Marker{AppID: m.ID, Name: m.Name, Version: m.Version, SHA256: map[string]string{},
		Purge: m.Purge, Installer: m.Maker}
	for _, f := range m.Files {
		p.Marker.Files = append(p.Marker.Files, f.Path)
		p.Marker.SHA256[f.Path] = f.SHA256
	}
	// the icon's and uninstaller's hashes are added by Apply, which has their bytes
	p.Marker.Files = append(p.Marker.Files, IconName, UninstallerName)
	for _, l := range p.Links {
		p.Marker.Shortcuts = append(p.Marker.Shortcuts, l.Path)
	}
	if p.Marker.Shortcuts == nil {
		p.Marker.Shortcuts = []string{} // "shortcuts": [] rather than null, so every reader gets a list
	}
	sort.Strings(p.Remove)
	sort.Strings(p.Kept)
	return p, nil
}

// links reports whether the plan writes a shortcut at path (case-insensitive, as Windows paths are).
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
// The rule comes from how the Microsoft C runtime splits a command line: 2n backslashes before a
// quote become n backslashes, and an odd one escapes the quote. So `C:\dir\` becomes `"C:\dir\\"`,
// which reads back as C:\dir\. Unlike syscall.EscapeArg it quotes even paths without spaces, so
// UninstallString always has the same shape.
func quoteArg(s string) string {
	n := len(s) - len(strings.TrimRight(s, `\`))
	return `"` + s + strings.Repeat(`\`, n) + `"`
}

// CheckFolder: setup installs only into a new or empty folder, or the folder of an earlier install of
// the same program, so it can never overwrite (or later remove) a file of the user's.
// id is the program's app id. Every problem is a *Refusal with Retry set, so the wizard's folder page
// can show it and let the user pick again; only an unexpected os.Stat error is returned plain.
//
// Steps:
//  1. Require a full path with a drive, at most 150 characters, and not a drive root.
//  2. Refuse the well-known folders themselves (a folder inside them is fine).
//  3. Accept a folder that does not exist yet.
//  4. Refuse a file; accept a folder whose install.json names this program; refuse one whose
//     marker names another program or cannot be read.
//  5. Otherwise accept the folder only if it is empty.
func CheckFolder(dir, id string, pl Places) error {
	if !filepath.IsAbs(dir) || filepath.VolumeName(dir) == "" {
		return refuse(true, "Choose a full path such as %s", pl.Dir("", "Example"))
	}
	clean := filepath.Clean(dir)
	// 150 keeps the folder well under the classic 260-character MAX_PATH, leaving room for the
	// program's own files below it and for .setup\new and .setup\old while installing
	if len(clean) > 150 {
		return refuse(true, "That folder's path is too long (more than 150 characters).")
	}
	if clean == filepath.VolumeName(clean)+`\` {
		return refuse(true, "Choose a folder, not the whole drive.")
	}
	// folders an install must never take over as a whole: an uninstall would empty them
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
// group and name give the default folder (Places.Dir). It returns "" and nil when neither place holds
// this program's marker; registry and marker read errors count as "not found". An install in a
// folder the user chose is found through InstallLocation, which the registry entry records.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=78a1e9bd41f901bb6fdce4d979b247b31f944bd969fcbcc87ea7ab1548d25396 ====
