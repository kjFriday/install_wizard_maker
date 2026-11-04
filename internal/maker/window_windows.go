// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/maker/window_windows.go
// About: The maker's window: menu, shortcuts, setup.json fields, loading, saving and commands.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The maker's window: menu, shortcuts, setup.json fields, loading, saving and commands.
//
// The window edits one setup.json at a time. Its fields mirror Form (form.go); the spec being edited
// is kept in makerUI.s and the fields are read back into a copy of it whenever the current state is
// needed. Building (with the -smoke walk) is in window_build_windows.go, the Help menu in
// window_help_windows.go. Controls come from package win, file pickers from package shell.
//
// Main flow:
//  1. Main (cli.go) calls Window with the setup.json named on the command line, if any.
//  2. Window makes the menu, the window and its controls (layout), loads the spec and runs the
//     message loop, or the -smoke walk instead.
//  3. Every menu item, accelerator and control notification arrives in command, which marks the
//     spec changed, opens pickers, loads, saves or builds.
//  4. Closing asks about unsaved changes (closing, mayDiscard) and refuses while a build runs.
//
// Start reading at Window.

package maker

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/shell"
	"generic-company/wizard/internal/spec"
	"generic-company/wizard/internal/sys"
	"generic-company/wizard/internal/win"
)

// The maker window's layout (96-dpi units) and command ids.
//
// mkW and mkH are the client size; labelX is the left column of captions, fieldX and fieldW the
// column of fields, and sideX and sideW the column of Browse and Add buttons on the right.
// package win scales these to the monitor's DPI.
const (
	mkW, mkH = 640, 566
	labelX   = 16
	fieldX   = 116
	fieldW   = 508
	sideX    = 536
	sideW    = 88
)

// The control and menu command ids, one per field, button and menu item. They start at 100 to stay
// clear of the dialog ids Windows reserves (IDOK = 1, IDCANCEL = 2 and so on). WM_COMMAND carries
// the id to command, and the accelerators and menu reuse the same ids, so Build from the menu, F5
// and the Build button all arrive as idBuild.
const (
	idName = 100 + iota
	idVersion
	idID
	idGroup
	idPublisher
	idIcon
	idIconBrowse
	idLicense
	idLicenseBrowse
	idFiles
	idAddFile
	idAddFolder
	idRemove
	idExclude
	idProgram
	idArgs
	idAskFolder
	idAskShortcuts
	idStartMenu
	idDesktop
	idOutput
	idOutputBrowse
	idBuild
	idLog
	idNew
	idOpen
	idSave
	idSaveAs
	idExit
	idRunSetup
	idAbout
	idInstallSelf
	idUninstallSelf
)

// jsonFilter is the file-type list of the Open and Save setup.json dialogs.
var jsonFilter = []shell.Filter{{Name: "setup.json", Pattern: "*.json"}, {Name: "All files", Pattern: "*.*"}}

// makerUI is the state of the maker's window: the spec being edited, its controls, and the build
// running behind it.
//
// Fields above mu belong to the UI thread; pending, done, result and err are shared with the build
// worker and only touched under mu. dirty is true once a field changed after the last load or save.
// Files are not a text field: s.Files is edited directly by Add file, Add folder and Remove.
type makerUI struct {
	w     *win.Window
	self  string
	file  string // the setup.json being edited; "" until saved
	s     *spec.Spec
	dirty bool
	quiet bool // filling the fields: changes are not edits

	name, version, id, group, publisher *win.Control
	icon, license, exclude, args        *win.Control
	files, program, output              *win.Control
	askFolder, askShortcuts             *win.Control
	startMenu, desktop                  *win.Control
	build, log                          *win.Control

	// UI thread only: the log so far, whether a build runs, and the last setup.exe built.
	logText   strings.Builder
	building  bool
	lastBuilt string

	// Shared with the build worker, under mu: new log lines, and the result once done.
	mu      sync.Mutex
	pending []string
	done    bool
	result  Result
	err     error
}

// Window opens the maker's window, with file (a setup.json) if given; smoke, if set, walks it instead.
//
// self is the maker's own exe (the stub of every build). The result is an app exit code: ExitOK
// after a normal close, ExitFailed when the window cannot be made or a panic ended the message loop,
// or the smoke walk's code. A setup.json that cannot be loaded is reported in a message box and the
// window opens untitled instead. It must run on the thread main locked, since Win32 windows belong
// to the thread that made them.
//
// Steps:
//  1. Build the File, Build and Help menus (a tab in an item's text puts its shortcut on the right).
//  2. Make the window, lay out the controls and register Ctrl+N, Ctrl+O, Ctrl+S and F5.
//  3. Hook commands, closing and worker wake-ups to the makerUI.
//  4. Load the spec (made absolute so the title and relative paths work) and fill the fields.
//  5. Run the smoke walk, or show the window with the name field focused and run the message loop.
func Window(self, file, smoke string) int {
	menu := win.NewMenu([]win.Menu{
		{Title: "&File", Items: []win.MenuItem{{ID: idNew, Text: "&New\tCtrl+N"}, {ID: idOpen, Text: "&Open…\tCtrl+O"},
			{ID: idSave, Text: "&Save\tCtrl+S"}, {ID: idSaveAs, Text: "Save &As…"}, {}, {ID: idExit, Text: "E&xit"}}},
		{Title: "&Build", Items: []win.MenuItem{{ID: idBuild, Text: "&Build setup.exe\tF5"}, {ID: idRunSetup, Text: "&Run the last setup.exe"}}},
		{Title: "&Help", Items: helpItems()},
	})
	w, err := win.New(app.Title, mkW, mkH, menu)
	if err != nil {
		sys.MessageBox(app.Name, err.Error(), sys.Error)
		return app.ExitFailed
	}
	ui := &makerUI{w: w, self: self}
	ui.layout()
	w.Accelerators([]win.Accel{{Key: 'N', Ctrl: true, ID: idNew}, {Key: 'O', Ctrl: true, ID: idOpen},
		{Key: 'S', Ctrl: true, ID: idSave}, {Key: win.VKF5, ID: idBuild}})
	w.OnCommand = ui.command
	w.OnClose = ui.closing
	w.OnWake = ui.wake
	s := spec.New("")
	if file != "" {
		if s, err = spec.Load(file); err != nil {
			w.MessageBox(app.Name, err.Error(), win.MBIconError)
			s, file = spec.New(""), ""
		} else if file, err = filepath.Abs(file); err != nil {
			file = ""
		}
	}
	ui.load(s, file)
	if smoke != "" {
		return ui.smoke(smoke)
	}
	w.Show()
	ui.name.Focus()
	w.Run()
	if w.Panic != nil {
		return app.ExitFailed
	}
	return app.ExitOK
}

// layout creates every control of the window at its fixed position (96-dpi units).
//
// The rows run top to bottom: name and version, app id and group, publisher, icon, license, the
// file list with its buttons, exclude, program and arguments, the four ticks, output, the Build
// button with its hint, and the log. "&" in a caption marks its access key (Alt+letter moves to the
// field after it). Build is the default button, so Enter in a field builds.
func (ui *makerUI) layout() {
	w := ui.w
	// row puts a caption in the left column; y+4 lines its text up with the 24-unit-high edit beside it
	row := func(y int, label string) {
		w.Caption(label, win.Rect{X: labelX, Y: y + 4, W: fieldX - labelX - 6, H: 20}, win.Face)
	}
	edit := func(id, x, y, width int) *win.Control { return w.Edit(id, "", win.Rect{X: x, Y: y, W: width, H: 24}) }

	row(14, "&Name")
	ui.name = edit(idName, fieldX, 14, 260)
	w.Caption("&Version", win.Rect{X: 392, Y: 18, W: 60, H: 20}, win.Face)
	ui.version = edit(idVersion, 456, 14, 168)
	row(44, "App i&d")
	ui.id = edit(idID, fieldX, 44, 260)
	w.Caption("&Group", win.Rect{X: 392, Y: 48, W: 60, H: 20}, win.Face)
	ui.group = edit(idGroup, 456, 44, 168)
	row(74, "&Publisher")
	ui.publisher = edit(idPublisher, fieldX, 74, fieldW)
	row(104, "&Icon (.ico)")
	ui.icon = edit(idIcon, fieldX, 104, 412)
	w.Button(idIconBrowse, "Browse…", win.Rect{X: sideX, Y: 103, W: sideW, H: 26})
	row(134, "&License")
	ui.license = edit(idLicense, fieldX, 134, 412)
	w.Button(idLicenseBrowse, "Browse…", win.Rect{X: sideX, Y: 133, W: sideW, H: 26})
	row(164, "&Files")
	ui.files = w.ListBox(idFiles, win.Rect{X: fieldX, Y: 164, W: 412, H: 104})
	w.Button(idAddFile, "Add fil&e…", win.Rect{X: sideX, Y: 163, W: sideW, H: 26})
	w.Button(idAddFolder, "Add f&older…", win.Rect{X: sideX, Y: 195, W: sideW, H: 26})
	w.Button(idRemove, "Re&move", win.Rect{X: sideX, Y: 227, W: sideW, H: 26})
	row(276, "E&xclude")
	ui.exclude = edit(idExclude, fieldX, 276, fieldW)
	row(306, "P&rogram")
	ui.program = w.Combo(idProgram, win.Rect{X: fieldX, Y: 306, W: 260, H: 200})
	w.Caption("&Arguments", win.Rect{X: 392, Y: 310, W: 62, H: 20}, win.Face)
	ui.args = edit(idArgs, 456, 306, 168)
	ui.askFolder = w.Check(idAskFolder, "Let people choose the folder", win.Rect{X: fieldX, Y: 338, W: 250, H: 24}, win.Face)
	ui.askShortcuts = w.Check(idAskShortcuts, "Ask about shortcuts", win.Rect{X: 376, Y: 338, W: 248, H: 24}, win.Face)
	ui.startMenu = w.Check(idStartMenu, "Start-menu shortcut ticked", win.Rect{X: fieldX, Y: 364, W: 250, H: 24}, win.Face)
	ui.desktop = w.Check(idDesktop, "Desktop shortcut ticked", win.Rect{X: 376, Y: 364, W: 248, H: 24}, win.Face)
	row(396, "O&utput")
	ui.output = edit(idOutput, fieldX, 396, 412)
	w.Button(idOutputBrowse, "Save as…", win.Rect{X: sideX, Y: 395, W: sideW, H: 26})
	ui.build = w.Button(idBuild, "&Build setup.exe", win.Rect{X: fieldX, Y: 430, W: 150, H: 28})
	w.Label("Builds what is saved; unsaved changes are saved first.", win.Rect{X: 276, Y: 436, W: 348, H: 20}, win.Normal, win.Face)
	ui.log = w.Text(idLog, "", win.Rect{X: labelX, Y: 468, W: mkW - 2*labelX, H: 84})
	w.SetDefault(idBuild)
}

// load shows a spec (from file, or untitled when file is "").
//
// It sets quiet while filling the fields, so the change notifications that SetText sends do not
// mark the spec changed, and clears dirty. It then refills the file list and the program choices
// and updates the title.
func (ui *makerUI) load(s *spec.Spec, file string) {
	ui.quiet = true
	defer func() { ui.quiet = false }()
	ui.s, ui.file, ui.dirty = s, file, false
	f := FormFrom(s)
	for c, v := range map[*win.Control]string{ui.name: f.Name, ui.version: f.Version, ui.id: f.ID, ui.group: f.Group,
		ui.publisher: f.Publisher, ui.icon: f.Icon, ui.license: f.License, ui.exclude: f.Exclude, ui.program: f.Program,
		ui.args: f.Args, ui.output: f.Output} {
		c.SetText(v)
	}
	ui.askFolder.SetChecked(f.AskFolder)
	ui.askShortcuts.SetChecked(f.AskShortcuts)
	ui.startMenu.SetChecked(f.StartMenu)
	ui.desktop.SetChecked(f.Desktop)
	ui.listFiles()
	ui.title()
}

// form reads the fields; Files comes from the spec, since the list box only shows it.
func (ui *makerUI) form() Form {
	return Form{Name: ui.name.TextValue(), Version: ui.version.TextValue(), ID: ui.id.TextValue(), Group: ui.group.TextValue(),
		Publisher: ui.publisher.TextValue(), Icon: ui.icon.TextValue(), License: ui.license.TextValue(), Files: ui.s.Files,
		Exclude: ui.exclude.TextValue(), Program: ui.program.TextValue(), Args: ui.args.TextValue(),
		AskFolder: ui.askFolder.Checked(), AskShortcuts: ui.askShortcuts.Checked(), StartMenu: ui.startMenu.Checked(),
		Desktop: ui.desktop.Checked(), Output: ui.output.TextValue()}
}

// current is the spec as the fields now say: a copy of ui.s with the form applied, so ui.s itself
// is left as loaded. The copy is shallow; Apply replaces its slices rather than changing them.
func (ui *makerUI) current() *spec.Spec {
	s := *ui.s
	ui.form().Apply(&s)
	return &s
}

// listFiles refreshes the file list from ui.s.Files and offers every .exe those rules pick up as a
// choice in the Program box.
//
// The program choices come from spec.Sources on the current fields, so they are the paths as they
// will sit in the install folder. When the sources cannot be listed (a missing folder, say) the
// choices are left empty. The text typed in the Program box is kept, since SetItems clears it.
func (ui *makerUI) listFiles() {
	var lines []string
	for _, r := range ui.s.Files {
		lines = append(lines, RuleText(r))
	}
	ui.files.SetItems(lines)
	var exes []string
	if src, err := ui.current().Sources(); err == nil {
		for _, f := range src {
			if strings.EqualFold(filepath.Ext(f.Path), ".exe") {
				exes = append(exes, f.Path)
			}
		}
	}
	text := ui.program.TextValue()
	ui.program.SetItems(exes)
	ui.program.SetText(text)
}

// title sets the window title: the app title, then the spec's folder and file name (or Untitled),
// with " *" while there are unsaved changes.
func (ui *makerUI) title() {
	name := "Untitled"
	if ui.file != "" {
		name = filepath.Base(filepath.Dir(ui.file)) + `\` + filepath.Base(ui.file)
	}
	if ui.dirty {
		name += " *"
	}
	ui.w.SetTitle(app.Title + " — " + name)
}

// changed marks the spec as edited and shows the " *" in the title, unless load is filling the
// fields or it is already marked.
func (ui *makerUI) changed() {
	if !ui.quiet && !ui.dirty {
		ui.dirty = true
		ui.title()
	}
}

// rel is how a picked path is written: relative to setup.json's folder when inside it.
//
// Relative paths use "/" so setup.json reads the same on any machine. An untitled spec has no
// folder yet, so its paths stay absolute until save relativizes them.
func (ui *makerUI) rel(p string) string {
	if ui.file == "" {
		return p
	}
	r, err := filepath.Rel(filepath.Dir(ui.file), p)
	if err != nil || !filepath.IsLocal(r) {
		return p
	}
	return filepath.ToSlash(r)
}

// dir is where the file pickers start: setup.json's folder, or the working folder while untitled.
func (ui *makerUI) dir() string {
	if ui.file != "" {
		return filepath.Dir(ui.file)
	}
	wd, _ := os.Getwd()
	return wd
}

// command handles one WM_COMMAND: id is the control, menu item or accelerator, and code the
// control's notification code (EN_CHANGE for an edit, CBN_* for the combo box).
//
// Edits and ticks only mark the spec changed. Browse buttons open a picker and write the chosen
// path, relative where possible; Add and Remove change ui.s.Files directly. The File and Build
// menus load, save, build and run; any other id goes to helpCommand. Errors show in message boxes.
func (ui *makerUI) command(id, code int) {
	switch id {
	case idName, idVersion, idID, idGroup, idPublisher, idIcon, idLicense, idExclude, idArgs, idOutput:
		if code == win.ENChange {
			ui.changed()
		}
	case idProgram:
		if code == win.CBNEditChange || code == win.CBNSelChange {
			ui.changed()
		}
	case idAskFolder, idAskShortcuts, idStartMenu, idDesktop:
		ui.changed()
	case idIconBrowse:
		if p, _ := shell.OpenFile(ui.w.HWND, "Icon", ui.dir(), []shell.Filter{{Name: "Icons", Pattern: "*.ico"}}); p != "" {
			ui.icon.SetText(ui.rel(p))
		}
	case idLicenseBrowse:
		if p, _ := shell.OpenFile(ui.w.HWND, "License", ui.dir(), []shell.Filter{{Name: "Text", Pattern: "*.txt;*.md;LICENSE*;COPYING*"}, {Name: "All files", Pattern: "*.*"}}); p != "" {
			ui.license.SetText(ui.rel(p))
		}
	case idOutputBrowse:
		if p, _ := shell.SaveFile(ui.w.HWND, "Write the setup.exe to", ui.dir(), "setup.exe", "exe", []shell.Filter{{Name: "Programs", Pattern: "*.exe"}}); p != "" {
			ui.output.SetText(ui.rel(p))
		}
	case idAddFile:
		if p, _ := shell.OpenFile(ui.w.HWND, "Add a file", ui.dir(), []shell.Filter{{Name: "All files", Pattern: "*.*"}}); p != "" {
			ui.addRule(spec.FileRule{From: ui.rel(p)})
		}
	case idAddFolder:
		if p, _ := shell.PickFolder(ui.w.HWND, "Add a folder's contents", ui.dir()); p != "" {
			ui.addRule(spec.FileRule{From: ui.rel(p)})
		}
	case idRemove:
		if i := ui.files.Selected(); i >= 0 && i < len(ui.s.Files) {
			// the full slice expression [:i:i] caps the head at i, so append copies into a new array
			// instead of shifting the old Files array in place
			ui.s.Files = append(ui.s.Files[:i:i], ui.s.Files[i+1:]...)
			ui.listFiles()
			ui.changed()
		}
	case idNew:
		if ui.mayDiscard() {
			ui.load(spec.New(""), "")
		}
	case idOpen:
		if !ui.mayDiscard() {
			return
		}
		if p, _ := shell.OpenFile(ui.w.HWND, "Open setup.json", ui.dir(), jsonFilter); p != "" {
			s, err := spec.Load(p)
			if err != nil {
				ui.w.MessageBox(app.Name, err.Error(), win.MBIconError)
				return
			}
			ui.load(s, p)
		}
	case idSave:
		ui.save(false)
	case idSaveAs:
		ui.save(true)
	case idBuild:
		ui.startBuild()
	case idRunSetup:
		if ui.lastBuilt == "" {
			ui.w.MessageBox(app.Name, "Build a setup.exe first (Build > Build setup.exe).", win.MBIconInfo)
		} else if err := shell.Open(ui.lastBuilt, "", filepath.Dir(ui.lastBuilt)); err != nil {
			ui.w.MessageBox(app.Name, err.Error(), win.MBIconError)
		}
	case idExit:
		if ui.closing() {
			ui.w.Close()
		}
	case idAbout:
		ui.w.MessageBox("About "+app.Name, about(), win.MBIconInfo)
	default:
		ui.helpCommand(id)
	}
}

// addRule appends a file rule, refreshes the list and marks the spec changed.
func (ui *makerUI) addRule(r spec.FileRule) {
	ui.s.Files = append(ui.s.Files, r)
	ui.listFiles()
	ui.changed()
}

// mayDiscard asks before unsaved changes are lost: false keeps them (and the window as it is).
//
// Yes saves (and is false when the save is cancelled or fails), No discards, Cancel keeps.
func (ui *makerUI) mayDiscard() bool {
	if !ui.dirty {
		return true
	}
	name := "this setup"
	if ui.file != "" {
		name = filepath.Base(ui.file)
	}
	switch ui.w.MessageBox(app.Name, "Save the changes to "+name+"?", win.MBYesNoCancel|win.MBIconQuestion) {
	case win.IDYES:
		return ui.save(false)
	case win.IDNO:
		return true
	}
	return false
}

// closing reports whether the window may close: never while a build runs (the worker still writes
// to the window), otherwise after mayDiscard. It is the window's OnClose and is also called by
// File > Exit and Help > Uninstall.
func (ui *makerUI) closing() bool {
	if ui.building {
		ui.w.MessageBox(app.Name, "Wait for the build to finish.", win.MBIconInfo)
		return false
	}
	return ui.mayDiscard()
}

// save writes setup.json; as, or an untitled spec, asks where. Paths inside its folder become relative.
//
// The result is true once the file is written; false when the dialog is cancelled or the write
// fails (shown in a message box).
//
// Steps:
//  1. Pick the target: the current file, or ask with a Save dialog.
//  2. Take the spec as the fields say.
//  3. When the folder changes (or the spec was untitled), make every path absolute from the old
//     folder, then relative to the new one, so the paths still point at the same files.
//  4. Write the file and load the saved spec, which clears dirty and updates the title.
func (ui *makerUI) save(as bool) bool {
	target := ui.file
	if as || target == "" {
		p, _ := shell.SaveFile(ui.w.HWND, "Save setup.json", ui.dir(), "setup.json", "json", jsonFilter)
		if p == "" {
			return false
		}
		target = p
	}
	s := ui.current()
	if ui.file == "" || !strings.EqualFold(filepath.Dir(target), filepath.Dir(ui.file)) {
		Absolutize(s)
		Relativize(s, filepath.Dir(target))
	}
	if err := s.Save(target); err != nil {
		ui.w.MessageBox(app.Name, err.Error(), win.MBIconError)
		return false
	}
	ui.load(s, target)
	return true
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=537850f12c43c9adcd152d4860f433790af823f90749dce7bfd5267cdfcb6358 ====
