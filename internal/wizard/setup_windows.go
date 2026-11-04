// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/wizard/setup_windows.go
// About: The install wizard's window: the pages a setup.exe shows when it runs without -quiet.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The install wizard's window: the pages a setup.exe shows when it runs without -quiet.
//
// RunSetup (setup.go) calls setupWindow once it holds the setup lock and the user did not ask for
// -quiet. The window is a frame (frame_windows.go) whose pages follow SetupFlow (flow.go): Welcome,
// License, Install Folder, Shortcuts, Installing and Finish, each one only when the manifest asks
// for it. The work itself is Setup.Check and Setup.Install (setup.go); this file only gathers the
// user's choices and shows the progress and the outcome.
//
// The main flow:
//  1. setupWindow makes the frame, builds every page's controls (build) and shows the first page.
//  2. command routes the buttons: nextPressed checks the current page's input and moves on;
//     cancelPressed asks before leaving or stopping.
//  3. On the page before Installing, Next runs Setup.Check again and start launches the install on a
//     worker goroutine.
//  4. The worker reports progress through Window.Wake; wake runs on the UI thread, moves the bar and,
//     at the end, fills the Finish page (or offers Retry for a refusal the user can put right).
//  5. finish starts the program when Launch is ticked and closes the window; setupWindow returns
//     ui.code, the process exit code.
//
// With -smoke, smoke walks the same pages by itself and writes a report instead of waiting for a user
// (main_test.go's TestWizardWindows reads it).
//
// Threading: every control is touched only on the UI thread (the thread main locked in init). The
// worker goroutine writes only the fields under mu and calls Wake.
//
// Start reading at setupWindow.

package wizard

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/install"
	"generic-company/wizard/internal/shell"
	"generic-company/wizard/internal/spec"
	"generic-company/wizard/internal/sys"
	"generic-company/wizard/internal/win"
)

// setupUI is the install wizard's window.
//
// It embeds the shared frame (window, flow, pages, Back/Next/Cancel) and adds the controls this
// wizard fills in or reads back, grouped by page in the order the pages come. The fields after mu
// are the worker's report to the UI thread.
type setupUI struct {
	*frame
	s   *Setup      // the run: manifest, payload, places, the user's choices
	out *sys.Output // the log (and, without a window, the message box); quiet while the window is up

	welcomeBody, welcomePath                    *win.Control
	accept                                      *win.Control
	folder, space, problem                      *win.Control
	startMenu, desktop                          *win.Control
	status, bar, detail                         *win.Control
	finishTitle, finishBody, finishPath, launch *win.Control

	// mu guards done, total, file, finished, res and err: the worker goroutine writes them and wake
	// reads them on the UI thread.
	mu       sync.Mutex
	done     int64 // bytes unpacked so far
	total    int64 // bytes to unpack in all; 0 until the worker's first report
	file     string
	finished bool // Install has returned; res and err hold its outcome
	res      install.Result
	err      error
	stop     atomic.Bool   // set by Cancel during the install; Install polls it through stop.Load
	hold     chan struct{} // -smoke: the worker waits for it, so the progress page is captured first
	code     int           // the exit code setupWindow returns (app.Exit...)
}

// setupWindow runs the install wizard's window for s and returns the process exit code.
//
// o supplies -smoke; out is the run's output, set quiet here because the window shows the outcome
// itself (a -log file still gets every line). The exit code starts as app.ExitCancelled, so closing
// the window before the install counts as a cancel.
//
// Steps:
//  1. Choose the pages (SetupFlow) from the manifest and whether this is an upgrade.
//  2. Make the frame; a failure here is reported through out, as no window exists.
//  3. Build every page's controls, hook up the window's callbacks and show the first page.
//  4. With -smoke, walk the pages by code (smoke); otherwise show the window and run its message
//     loop until it closes.
//  5. A panic caught in a window callback (Window.Panic) is logged and counts as a failure.
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
	// the close box acts as Cancel; false keeps the window open, and cancelPressed closes it itself
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

// build makes every page's controls up front and files each under its page; frame.show then shows
// one page's set at a time.
//
// Positions are in the frame's 96-dpi units (frame_windows.go): interior pages use the area below
// the white header (Y 70 and down, X 20 to 483), exterior pages the area right of the banner
// (bodyX, bodyW). The "&" in a caption marks its Alt-key access letter.
//
// Steps:
//  1. Welcome: the title, a body set by the checks below, and the folder when it is already settled.
//  2. Run Setup.Check once, so a problem the user cannot fix on a later page blocks setup here.
//  3. License: the text and the "I accept" box that gates Next.
//  4. Install Folder: the folder box with Browse, the space line and a problem line.
//  5. Shortcuts: the two boxes, ticked from the defaults (or the earlier install's choices).
//  6. Installing: a status line, the progress bar and the current file.
//  7. Finish: title and body (filled in by wake), the folder and Launch; then the buttons, last so
//     Tab reaches them last.
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

// flowHasFolder reports whether this run shows the Install Folder page (setup.json's askFolder,
// and not an upgrade, which keeps its folder).
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

// welcomeText is the welcome page's body for the three cases: a new install, a repair (the same
// version installed) and an upgrade (an older version installed). A downgrade never gets here:
// openSetup refuses it.
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

// updateSpace refreshes the folder page's line with the space the program needs and, when the drive
// of the typed folder can be read, the space free on it. It is called as the folder text changes.
func (ui *setupUI) updateSpace() {
	need := ui.s.M.Size()
	text := fmt.Sprintf("Space needed: %s.", human(need))
	if free, err := sys.FreeSpace(strings.TrimSpace(ui.folder.TextValue())); err == nil {
		text += fmt.Sprintf(" Free on that drive: %s.", human(int64(free)))
	}
	ui.space.SetText(text)
}

// command handles a WM_COMMAND from the window: id is the control (or IDOK/IDCANCEL for Next and
// Cancel, which Enter and Esc also send) and code its notification code.
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
		// EN_CHANGE: the user edited the folder box; an old problem no longer applies
		if code == win.ENChange {
			ui.problem.SetText("")
			ui.updateSpace()
		}
	}
}

// browse opens the folder picker at the parent of the typed folder and puts the choice in the folder
// box, with the program's name appended unless the picked folder already ends in it. Cancelling the
// picker, or an error, leaves the box as it was.
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

// nextPressed handles Next (or Install, or Finish, by the page).
//
// Steps:
//  1. Ignore it while Next is disabled: IDOK can also come from the keyboard (Enter, through
//     IsDialogMessage), not only from the button.
//  2. Read back the current page's input: the folder must pass install.CheckFolder (else the problem
//     shows under the box and Next stays on the page); the shortcut boxes go into Choices.
//  3. On the last page, finish. On the page before Installing, check everything once more (the
//     folder or Python may have changed since build) and start the install. Otherwise move on.
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

// cancelPressed handles Cancel and the close box.
//
// On the Finish page it acts as Finish (Launch included). While installing it asks, then sets stop
// and disables Cancel; the window stays open until the worker ends and wake shows the outcome. Before
// the install it asks, then closes with app.ExitCancelled.
func (ui *setupUI) cancelPressed() {
	switch {
	case ui.flow.Last():
		ui.finish()
	case ui.flow.Working():
		// install.Apply honours stop only while unpacking; once files move into place it runs to the end
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
//
// It is also called again for Retry, so it resets the shared fields first. The bar runs 0 to 1000 so
// byte counts above the progress control's int range still map onto it.
//
// Steps:
//  1. Show what is happening (install or upgrade) and log it.
//  2. Reset the worker's fields under mu.
//  3. On the worker: wait for hold under -smoke, run Setup.Install with a progress callback that
//     stores the counts and wakes the UI thread, then store the outcome and wake it once more.
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

// wake runs on the UI thread after the worker calls Window.Wake: it shows the progress or, once the
// install has ended, its outcome.
//
// Steps:
//  1. Copy the worker's fields under mu, so the lock is not held while controls repaint.
//  2. Still running: move the bar (in thousandths) and show the current file.
//  3. Ended: a refusal with Retry (a file in use, too little space) asks Retry or Cancel; Retry starts
//     again. Under -smoke (hold set) there is no one to ask, so the outcome stands.
//  4. Set the exit code and fill the Finish page for success, cancel or failure; Launch shows only
//     after a success.
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
	if ui.flow.Page() != Progress { // the outcome was shown already; a late wake-up changes nothing
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

// finish closes the wizard, first starting the program when the install succeeded and Launch is
// ticked. LaunchTarget prefers the new shortcut, so the program starts with its own taskbar identity
// and working folder; a failure to start it is shown but does not change the exit code.
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
//
// file is the JSON report; the PNG captures go beside it (win.NewSmoke). The result is the exit code
// the walk ended with, or app.ExitFailed when the report cannot be written. The capture names
// ("welcome", "license", "license-accepted", ... "finish") are what TestWizardWindows expects.
//
// Steps:
//  1. Set hold, so the worker waits until the Installing page has been captured.
//  2. Loop over the pages, at most 20 steps, so a page that never moves on cannot hang the walk.
//  3. License: capture before and after ticking "I accept". Installing: capture, release the
//     worker, pump messages until the page changes. Finish: untick Launch, capture, press Finish.
//     Any other page: capture and press Next, or leave with app.ExitRefused when Next is disabled.
//  4. Close the window and write the report.
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
				break // leaves the switch; the loop then ends because the window is closed
			}
			ui.next.Click()
		}
	}
	ui.w.Close() // a no-op when the walk already closed it
	if err := s.Write(ui.code); err != nil {
		return app.ExitFailed
	}
	return ui.code
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=21754cbc7cef6331f6d1105876c71e9fbd5682c1b7621752c6b59411138e059e ====
