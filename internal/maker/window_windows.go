package maker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"acme/wizard/internal/app"
	"acme/wizard/internal/shell"
	"acme/wizard/internal/spec"
	"acme/wizard/internal/sys"
	"acme/wizard/internal/win"
)

// The maker window's layout (96-dpi units) and command ids.
const (
	mkW, mkH = 640, 566
	labelX   = 16
	fieldX   = 116
	fieldW   = 508
	sideX    = 536
	sideW    = 88
)

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

var jsonFilter = []shell.Filter{{Name: "setup.json", Pattern: "*.json"}, {Name: "All files", Pattern: "*.*"}}

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

	logText   strings.Builder
	building  bool
	lastBuilt string

	mu      sync.Mutex
	pending []string
	done    bool
	result  Result
	err     error
}

// Window opens the maker's window, with file (a setup.json) if given; smoke, if set, walks it instead.
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
		{Key: 'S', Ctrl: true, ID: idSave}, {Key: 0x74, ID: idBuild}}) // F5
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

func (ui *makerUI) layout() {
	w := ui.w
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

// form reads the fields.
func (ui *makerUI) form() Form {
	return Form{Name: ui.name.TextValue(), Version: ui.version.TextValue(), ID: ui.id.TextValue(), Group: ui.group.TextValue(),
		Publisher: ui.publisher.TextValue(), Icon: ui.icon.TextValue(), License: ui.license.TextValue(), Files: ui.s.Files,
		Exclude: ui.exclude.TextValue(), Program: ui.program.TextValue(), Args: ui.args.TextValue(),
		AskFolder: ui.askFolder.Checked(), AskShortcuts: ui.askShortcuts.Checked(), StartMenu: ui.startMenu.Checked(),
		Desktop: ui.desktop.Checked(), Output: ui.output.TextValue()}
}

// current is the spec as the fields now say.
func (ui *makerUI) current() *spec.Spec {
	s := *ui.s
	ui.form().Apply(&s)
	return &s
}

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

func (ui *makerUI) changed() {
	if !ui.quiet && !ui.dirty {
		ui.dirty = true
		ui.title()
	}
}

// rel is how a picked path is written: relative to setup.json's folder when inside it.
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

func (ui *makerUI) dir() string {
	if ui.file != "" {
		return filepath.Dir(ui.file)
	}
	wd, _ := os.Getwd()
	return wd
}

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

func (ui *makerUI) addRule(r spec.FileRule) {
	ui.s.Files = append(ui.s.Files, r)
	ui.listFiles()
	ui.changed()
}

// mayDiscard asks before unsaved changes are lost: false keeps them (and the window as it is).
func (ui *makerUI) mayDiscard() bool {
	if !ui.dirty {
		return true
	}
	name := "this setup"
	if ui.file != "" {
		name = filepath.Base(ui.file)
	}
	switch ui.w.MessageBox(app.Name, "Save the changes to "+name+"?", 0x03|win.MBIconQuestion) { // MB_YESNOCANCEL
	case win.IDYES:
		return ui.save(false)
	case win.IDNO:
		return true
	}
	return false
}

func (ui *makerUI) closing() bool {
	if ui.building {
		ui.w.MessageBox(app.Name, "Wait for the build to finish.", win.MBIconInfo)
		return false
	}
	return ui.mayDiscard()
}

// save writes setup.json; as, or an untitled spec, asks where. Paths inside its folder become relative.
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

// startBuild saves if needed (a build is always of setup.json as saved), then builds on a worker.
func (ui *makerUI) startBuild() {
	if ui.building {
		return
	}
	if (ui.dirty || ui.file == "") && !ui.save(false) {
		return
	}
	stub, err := os.ReadFile(ui.self)
	if err != nil {
		ui.w.MessageBox(app.Name, err.Error(), win.MBIconError)
		return
	}
	ui.logText.Reset()
	ui.log.SetText("")
	ui.building = true
	ui.build.Enable(false)
	ui.mu.Lock()
	ui.done, ui.pending = false, nil
	ui.mu.Unlock()
	s := ui.s
	go func() {
		res, err := Build(s, stub, func(format string, a ...any) {
			ui.mu.Lock()
			ui.pending = append(ui.pending, fmt.Sprintf(format, a...))
			ui.mu.Unlock()
			ui.w.Wake()
		})
		ui.mu.Lock()
		ui.result, ui.err, ui.done = res, err, true
		ui.mu.Unlock()
		ui.w.Wake()
	}()
}

func (ui *makerUI) wake() {
	ui.mu.Lock()
	lines, done, res, err := ui.pending, ui.done, ui.result, ui.err
	ui.pending = nil
	ui.mu.Unlock()
	for _, l := range lines {
		ui.logText.WriteString(l + "\n")
	}
	if done && ui.building {
		ui.building = false
		ui.build.Enable(true)
		var se *SpecError
		switch {
		case errors.As(err, &se):
			ui.logText.WriteString(filepath.Base(ui.file) + " has problems:\n  " + strings.ReplaceAll(err.Error(), "\n", "\n  ") + "\n")
		case err != nil:
			ui.logText.WriteString("Build failed: " + err.Error() + "\n")
		default:
			ui.lastBuilt = res.Path
			ui.logText.WriteString(fmt.Sprintf("Done: %d files in %s. Build > Run the last setup.exe tries it.\n", res.Files, filepath.Base(res.Path)))
		}
	}
	ui.log.SetText(ui.logText.String())
}

// smoke shows the window with its setup.json, builds it, and records both.
func (ui *makerUI) smoke(file string) int {
	s := win.NewSmoke(ui.w, file, "maker", app.Title)
	ui.w.Show()
	s.Capture("form", nil)
	ui.startBuild()
	s.WaitFor(func() bool { return !ui.building }) // cleared on the UI thread once the result is shown
	s.Capture("built", map[string]bool{"built": ui.lastBuilt != ""})
	ui.w.Close()
	code := app.ExitOK
	if ui.lastBuilt == "" {
		code = app.ExitFailed
	}
	if err := s.Write(code); err != nil {
		return app.ExitFailed
	}
	return code
}

func about() string {
	return app.Title + "\n\nMakes install wizards: one setup.exe per program, which installs it for the current user, " +
		"lists it in Apps & features and leaves an uninstaller.\n\n" + app.Credit +
		"\n\nCode: GNU AGPL-3.0. ACME Inc. is fictional."
}

// helpItems are the Help menu's entries.
func helpItems() []win.MenuItem {
	return []win.MenuItem{{ID: idInstallSelf, Text: "&Install for this user…"}, {ID: idUninstallSelf, Text: "&Uninstall…"}, {},
		{ID: idAbout, Text: "&About"}}
}

// helpCommand handles the Help menu's install entries: each starts its own window.
func (ui *makerUI) helpCommand(id int) {
	var err error
	switch id {
	case idInstallSelf:
		err = startSelfSetup(ui.self)
	case idUninstallSelf:
		err = startUninstaller()
	}
	if err != nil {
		ui.w.MessageBox(app.Name, err.Error(), win.MBIconWarning)
	}
}
