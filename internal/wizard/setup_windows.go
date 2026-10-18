package wizard

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"acme/wizard/internal/app"
	"acme/wizard/internal/install"
	"acme/wizard/internal/shell"
	"acme/wizard/internal/spec"
	"acme/wizard/internal/sys"
	"acme/wizard/internal/win"
)

// setupUI is the install wizard's window.
type setupUI struct {
	*frame
	s   *Setup
	out *sys.Output

	welcomeBody, welcomePath                    *win.Control
	accept                                      *win.Control
	folder, space, problem                      *win.Control
	startMenu, desktop                          *win.Control
	status, bar, detail                         *win.Control
	finishTitle, finishBody, finishPath, launch *win.Control

	mu       sync.Mutex
	done     int64
	total    int64
	file     string
	finished bool
	res      install.Result
	err      error
	stop     atomic.Bool
	hold     chan struct{} // -smoke: the worker waits for it, so the progress page is captured first
	code     int
}

func setupWindow(s *Setup, o setupOptions, out *sys.Output) int {
	m := s.M
	flow := SetupFlow(m.License != "", m.AskFolder, m.Shortcuts.Ask, s.Upgrade())
	f, err := newFrame(m.Name+" "+m.Version+" Setup", flow, s.Icon)
	if err != nil {
		out.Printf("%v", err)
		out.Done(sys.Error)
		return app.ExitFailed
	}
	out.SetQuiet(true) // the window says it all; the log, if any, still gets every line
	ui := &setupUI{frame: f, s: s, out: out, code: app.ExitCancelled}
	ui.build()
	f.w.OnCommand = ui.command
	f.w.OnClose = func() bool { ui.cancelPressed(); return false }
	f.w.OnWake = ui.wake
	ui.show()
	if o.smoke != "" {
		return ui.smoke(o.smoke)
	}
	f.w.Show()
	f.w.Run()
	if f.w.Panic != nil {
		out.Printf("%v", f.w.Panic)
		return app.ExitFailed
	}
	return ui.code
}

func (ui *setupUI) build() {
	w, m, s := ui.w, ui.s.M, ui.s
	_, ui.welcomeBody = ui.exterior(Welcome, fmt.Sprintf("Welcome to the %s Setup Wizard", m.Name), "")
	ui.welcomePath = w.PathLabel("", win.Rect{X: bodyX, Y: 240, W: bodyW, H: 20}, win.Paper)
	ui.add(Welcome, ui.welcomePath)
	if s.Upgrade() || !ui.flowHasFolder() { // the folder is settled already: say which
		ui.welcomePath.SetText("Folder: " + s.Choices.Dir)
	}
	switch err := s.Check(); {
	case err != nil && s.M.Run.Program == spec.Python && s.Python == "":
		ui.block(err) // no Python: nothing to choose on a later page will help
	case err != nil && !ui.flowHasFolder():
		ui.block(err) // the folder is wrong and cannot be changed here
	default:
		ui.welcomeBody.SetText(ui.welcomeText()) // a folder problem shows on the folder page
	}

	ui.header(License, "License Agreement", fmt.Sprintf("Please read the license of %s before installing it.", m.Name))
	ui.add(License, w.Text(0, m.License, win.Rect{X: 20, Y: 70, W: 463, H: 196}))
	ui.accept = w.Check(idAccept, "I &accept the terms of the license", win.Rect{X: 20, Y: 276, W: 463, H: 24}, win.Face)
	ui.add(License, ui.accept)

	ui.header(Folder, "Install Folder", fmt.Sprintf("Where should %s go?", m.Name))
	ui.add(Folder, w.Label(fmt.Sprintf("Setup installs %s in the folder below, for you alone. To use another one, type it or click Browse; it must be new or empty.", m.Name),
		win.Rect{X: 20, Y: 70, W: 463, H: 40}, win.Normal, win.Face))
	ui.folder = w.Edit(idFolder, s.Choices.Dir, win.Rect{X: 20, Y: 116, W: 372, H: 24})
	ui.add(Folder, ui.folder, w.Button(idBrowse, "B&rowse…", win.Rect{X: 400, Y: 115, W: 83, H: 26}))
	ui.space = w.Label("", win.Rect{X: 20, Y: 150, W: 463, H: 20}, win.Normal, win.Face)
	ui.problem = w.Label("", win.Rect{X: 20, Y: 178, W: 463, H: 60}, win.Bold, win.Face)
	ui.add(Folder, ui.space, ui.problem)
	ui.updateSpace()

	ui.header(Shortcuts, "Shortcuts", fmt.Sprintf("Where should %s be easy to find?", m.Name))
	where := "Start menu"
	if m.Group != "" {
		where = "Start menu, under " + m.Group
	}
	ui.startMenu = w.Check(idStartMenu, "Add a shortcut to the &"+where, win.Rect{X: 20, Y: 76, W: 463, H: 24}, win.Face)
	ui.desktop = w.Check(idDesktop, "Add a shortcut to the &desktop", win.Rect{X: 20, Y: 106, W: 463, H: 24}, win.Face)
	ui.startMenu.SetChecked(s.Choices.StartMenu)
	ui.desktop.SetChecked(s.Choices.Desktop)
	ui.add(Shortcuts, ui.startMenu, ui.desktop)

	ui.header(Progress, "Installing", fmt.Sprintf("Please wait while %s is installed.", m.Name))
	ui.status = w.Label("Getting ready…", win.Rect{X: 20, Y: 76, W: 463, H: 20}, win.Normal, win.Face)
	ui.bar = w.Progress(win.Rect{X: 20, Y: 102, W: 463, H: 18})
	ui.detail = w.PathLabel("", win.Rect{X: 20, Y: 128, W: 463, H: 20}, win.Face)
	ui.add(Progress, ui.status, ui.bar, ui.detail)

	ui.finishTitle, ui.finishBody = ui.exterior(Finish, "", "")
	ui.finishPath = w.PathLabel("", win.Rect{X: bodyX, Y: 240, W: bodyW, H: 20}, win.Paper)
	ui.add(Finish, ui.finishPath)
	ui.launch = w.Check(idLaunch, "&Launch "+m.Name, win.Rect{X: bodyX, Y: 272, W: bodyW, H: 24}, win.Paper)
	ui.launch.SetChecked(true)
	ui.add(Finish, ui.launch)
	ui.buttons()
}

func (ui *setupUI) flowHasFolder() bool {
	for _, p := range ui.flow.Pages() {
		if p == Folder {
			return true
		}
	}
	return false
}

// block shows why setup cannot go on, on the welcome page, and keeps Next disabled.
func (ui *setupUI) block(err error) {
	ui.gate.Blocked = true
	ui.welcomeBody.SetText(err.Error() + "\n\nClick Cancel to leave.")
}

func (ui *setupUI) welcomeText() string {
	m, s := ui.s.M, ui.s
	switch {
	case s.Old == nil:
		return fmt.Sprintf("This installs %s %s by %s, for you alone: no administrator rights are needed.\n\n"+
			"Click Next to continue, or Cancel to leave setup.", m.Name, m.Version, m.Publisher)
	case spec.CompareVersions(m.Version, s.Old.Version) == 0:
		return fmt.Sprintf("%s %s is already installed. Setup will repair it: the files it installed are put back as they were; files you changed are kept.\n\n"+
			"Click Next to continue.", m.Name, m.Version)
	}
	return fmt.Sprintf("%s %s is installed. Setup will upgrade it to %s in the same folder; files you changed or added there are kept.\n\n"+
		"Click Next to continue.", m.Name, s.Old.Version, m.Version)
}

func (ui *setupUI) updateSpace() {
	need := ui.s.M.Size()
	text := fmt.Sprintf("Space needed: %s.", human(need))
	if free, err := sys.FreeSpace(strings.TrimSpace(ui.folder.TextValue())); err == nil {
		text += fmt.Sprintf(" Free on that drive: %s.", human(int64(free)))
	}
	ui.space.SetText(text)
}

func (ui *setupUI) command(id, code int) {
	switch id {
	case win.IDOK:
		ui.nextPressed()
	case idBack:
		if ui.flow.Back() {
			ui.show()
		}
	case win.IDCANCEL:
		ui.cancelPressed()
	case idAccept:
		ui.gate.Accepted = ui.accept.Checked()
		ui.show()
	case idBrowse:
		ui.browse()
	case idFolder:
		if code == win.ENChange {
			ui.problem.SetText("")
			ui.updateSpace()
		}
	}
}

func (ui *setupUI) browse() {
	start := strings.TrimSpace(ui.folder.TextValue())
	picked, err := shell.PickFolder(ui.w.HWND, "Install "+ui.s.M.Name+" in…", filepath.Dir(start))
	if err != nil || picked == "" {
		return
	}
	if !strings.EqualFold(filepath.Base(picked), ui.s.M.Name) {
		picked = filepath.Join(picked, ui.s.M.Name) // the program gets a folder of its own
	}
	ui.folder.SetText(picked)
}

func (ui *setupUI) nextPressed() {
	if !ui.next.Enabled() {
		return
	}
	switch ui.flow.Page() {
	case Folder:
		dir, err := filepath.Abs(strings.TrimSpace(ui.folder.TextValue()))
		if err == nil {
			err = install.CheckFolder(dir, ui.s.M.ID, ui.s.Places)
		}
		if err != nil {
			ui.problem.SetText(err.Error())
			ui.folder.Focus()
			return
		}
		ui.s.Choices.Dir = dir
	case Shortcuts:
		ui.s.Choices.StartMenu, ui.s.Choices.Desktop = ui.startMenu.Checked(), ui.desktop.Checked()
	}
	switch {
	case ui.flow.Last():
		ui.finish()
	case ui.flow.BeforeWork():
		if err := ui.s.Check(); err != nil {
			ui.w.MessageBox(ui.title, err.Error(), win.MBIconWarning)
			return
		}
		ui.flow.Next()
		ui.show()
		ui.start()
	default:
		ui.flow.Next()
		ui.show()
	}
}

func (ui *setupUI) cancelPressed() {
	switch {
	case ui.flow.Last():
		ui.finish()
	case ui.flow.Working():
		if !ui.stop.Load() && ui.ask("Stop installing? Setup will put back everything it changed.") {
			ui.stop.Store(true)
			ui.cancel.Enable(false)
			ui.status.SetText("Cancelling: putting things back…")
		}
	default:
		if ui.ask("Leave setup? Nothing has been installed.") {
			ui.code = app.ExitCancelled
			ui.w.Close()
		}
	}
}

// start runs the install on a worker goroutine; wake shows its progress and its end.
func (ui *setupUI) start() {
	m := ui.s.M
	verb := "Installing"
	if ui.s.Upgrade() {
		verb = "Upgrading"
	}
	ui.status.SetText(fmt.Sprintf("%s %s %s…", verb, m.Name, m.Version))
	ui.detail.SetText(ui.s.Choices.Dir)
	ui.out.Printf("%s %s %s in %s", verb, m.Name, m.Version, ui.s.Choices.Dir)
	ui.bar.SetProgress(0, 1000)
	ui.mu.Lock()
	ui.finished, ui.done, ui.total, ui.file = false, 0, 0, ""
	ui.mu.Unlock()
	go func() {
		if ui.hold != nil {
			<-ui.hold
		}
		res, err := ui.s.Install(func(done, total int64, file string) {
			ui.mu.Lock()
			ui.done, ui.total, ui.file = done, total, file
			ui.mu.Unlock()
			ui.w.Wake()
		}, ui.stop.Load)
		ui.mu.Lock()
		ui.res, ui.err, ui.finished = res, err, true
		ui.mu.Unlock()
		ui.w.Wake()
	}()
}

func (ui *setupUI) wake() {
	ui.mu.Lock()
	done, total, file, finished, res, err := ui.done, ui.total, ui.file, ui.finished, ui.res, ui.err
	ui.mu.Unlock()
	if !finished {
		if total > 0 {
			ui.bar.SetProgress(int(done*1000/total), 1000)
		}
		ui.detail.SetText(file)
		return
	}
	if ui.flow.Page() != Progress {
		return
	}
	var r *install.Refusal
	if errors.As(err, &r) && r.Retry && ui.hold == nil {
		if ui.w.MessageBox(ui.title, r.Msg, win.MBRetryCancel|win.MBIconWarning) == win.IDRETRY {
			ui.start()
			return
		}
	}
	ui.code = exitCode(err)
	ui.bar.SetProgress(1000, 1000)
	m := ui.s.M
	switch {
	case err == nil:
		ui.out.Printf("Installed %s %s.", m.Name, m.Version)
		ui.finishTitle.SetText(fmt.Sprintf("%s is installed", m.Name))
		text := fmt.Sprintf("Setup has installed %s %s.", m.Name, m.Version)
		if len(res.Kept) > 0 {
			text += "\n\nKept as they were (changed since installed, or not installed by this setup): " + strings.Join(res.Kept, ", ") + "."
		}
		ui.finishBody.SetText(text + "\n\nClick Finish to close setup.")
		ui.finishPath.SetText("Folder: " + ui.s.Choices.Dir)
	case errors.Is(err, install.ErrCancelled):
		ui.out.Printf("Cancelled.")
		ui.finishTitle.SetText("Setup was cancelled")
		ui.finishBody.SetText("Nothing was changed.\n\nClick Finish to close setup.")
	default:
		ui.out.Printf("%v", err)
		ui.finishTitle.SetText("Setup did not finish")
		ui.finishBody.SetText(err.Error() + "\n\nEverything setup changed was put back.\n\nClick Finish to close setup.")
	}
	ui.flow.Next()
	ui.show()
	ui.launch.Show(err == nil)
}

func (ui *setupUI) finish() {
	if ui.code == app.ExitOK && ui.launch.Visible() && ui.launch.Checked() {
		file, args, dir := ui.s.LaunchTarget()
		if err := shell.Open(file, args, dir); err != nil {
			ui.w.MessageBox(ui.title, fmt.Sprintf("Could not start %s: %v", ui.s.M.Name, err), win.MBIconWarning)
		}
	}
	ui.w.Close()
}

// smoke walks every page as a user would, capturing each, with the license accepted, the defaults
// kept and Launch unticked; the install runs for real (so pass -root).
func (ui *setupUI) smoke(file string) int {
	s := win.NewSmoke(ui.w, file, "setup", ui.title)
	ui.hold = make(chan struct{})
	ui.w.Show()
	for steps := 0; !ui.w.Closed() && steps < 20; steps++ {
		switch page := ui.flow.Page(); page {
		case License:
			s.Capture("license", ui.flags())
			ui.accept.Click()
			s.Capture("license-accepted", ui.flags())
			ui.next.Click()
		case Progress:
			s.Capture("progress", ui.flags())
			close(ui.hold)
			s.WaitFor(func() bool { return ui.flow.Page() != Progress })
		case Finish:
			ui.launch.SetChecked(false)
			s.Capture("finish", ui.flags())
			ui.next.Click()
		default:
			s.Capture(page.String(), ui.flags())
			if !ui.next.Enabled() { // blocked: leave the way a user would
				ui.code = app.ExitRefused
				ui.w.Close()
				break
			}
			ui.next.Click()
		}
	}
	ui.w.Close()
	if err := s.Write(ui.code); err != nil {
		return app.ExitFailed
	}
	return ui.code
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

func quoteForShell(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// human is a size for people.
func human(n int64) string {
	switch {
	case n < 1024*1024:
		return fmt.Sprintf("%d KB", (n+1023)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
}
