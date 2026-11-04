// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/wizard/uninstall_windows.go
// About: The uninstaller's window: the Confirm, Removing and Done pages.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The uninstaller's window: the Confirm, Removing and Done pages.
//
// An uninstall.exe shows it when run without -quiet. RunUninstall (uninstall.go) calls uninstallWindow once it holds the lock shared with setup. The
// window is the same frame as the install wizard's (frame_windows.go), with UninstallFlow's three
// pages. The work is Uninstaller.Remove; this file shows the choice to purge settings, the progress
// and the outcome.
//
// The main flow:
//  1. uninstallWindow builds the pages and shows Confirm.
//  2. Uninstall (Next) starts Remove on a worker goroutine and disables Cancel: a removal is not
//     undone half-way.
//  3. wake, on the UI thread, moves the bar and at the end fills the Done page.
//  4. When the window closes, uninstallWindow releases the lock and, after a success, starts the
//     removal of the exe itself (RemoveAfterExit) as the very last step.
//
// Threading: as in the install wizard, the worker writes only the fields under mu and calls Wake.

package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/install"
	"generic-company/wizard/internal/sys"
	"generic-company/wizard/internal/win"
)

// uninstallUI is the uninstaller's window: Confirm, Removing, Done.
type uninstallUI struct {
	*frame
	u   *Uninstaller
	out *sys.Output

	purge               *win.Control
	status, bar, detail *win.Control
	doneTitle, doneBody *win.Control

	// mu guards done, total, file, finished, res and err: the worker goroutine writes them and wake
	// reads them on the UI thread.
	mu       sync.Mutex
	done     int64 // marker entries handled so far
	total    int64 // marker entries in all
	file     string
	finished bool // Remove has returned; res and err hold its outcome
	res      install.Result
	err      error
	hold     chan struct{} // -smoke: the worker waits for it, so the removing page is captured first
	code     int           // the exit code uninstallWindow returns (app.Exit...)
}

// uninstallWindow runs the uninstaller's window for u and returns the process exit code.
//
// release frees the lock RunUninstall took; it is called on every path, before RemoveAfterExit. The
// exit code starts as app.ExitCancelled, so closing on the Confirm page counts as a cancel.
//
// Steps:
//  1. Read the program's icon (app.ico) from the install folder, drawn on the banner and header; a
//     read error is ignored.
//  2. Make the frame; a failure is reported through out, as no window exists.
//  3. Build the pages, hook up the callbacks, show Confirm, then walk it (-smoke) or run the message
//     loop.
//  4. Release the lock; after a finished, successful removal, hand the exe and its folders to
//     RemoveAfterExit.
func uninstallWindow(u *Uninstaller, o uninstallOptions, out *sys.Output, release func()) int {
	ico, _ := os.ReadFile(filepath.Join(u.Dir, install.IconName))
	f, err := newFrame(u.Info.Name+" Uninstall", UninstallFlow(), ico)
	if err != nil {
		release()
		out.Printf("%v", err)
		out.Done(sys.Error)
		return app.ExitFailed
	}
	out.SetQuiet(true) // the window shows the outcome; the log, if any, still gets every line
	ui := &uninstallUI{frame: f, u: u, out: out, code: app.ExitCancelled}
	ui.build()
	f.w.OnCommand = ui.command
	// the close box acts as Cancel; false keeps the window open, and cancelPressed closes it itself
	f.w.OnClose = func() bool { ui.cancelPressed(); return false }
	f.w.OnWake = ui.wake
	ui.show()
	if o.smoke != "" {
		ui.smoke(o.smoke)
	} else {
		f.w.Show()
		f.w.Run()
	}
	release()
	if ui.finished && ui.err == nil {
		RemoveAfterExit(ui.res) // the very last step: remove this exe once it has exited
	}
	return ui.code
}

// build makes the three pages' controls up front, in the frame's 96-dpi units.
//
// Confirm says what goes and what stays, shows the folder and offers to purge settings; the purge
// box is disabled, and says why, when the marker lists no settings folders. Removing has a status
// line, the bar and the current entry. Done's title and body are filled in by wake. The buttons come
// last, so Tab reaches them last.
func (ui *uninstallUI) build() {
	w, info := ui.w, ui.u.Info
	ui.header(Confirm, "Uninstall "+info.Name, fmt.Sprintf("%s %s will be removed from your computer.", info.Name, info.Version))
	ui.add(Confirm, w.Label("This removes what setup installed, its shortcuts and its entry in Apps & features. "+
		"Files you changed or added are kept, and so is any data the Generic Company programs share.",
		win.Rect{X: 20, Y: 72, W: 463, H: 60}, win.Normal, win.Face),
		w.PathLabel("Folder: "+ui.u.Dir, win.Rect{X: 20, Y: 140, W: 463, H: 20}, win.Face))
	ui.purge = w.Check(idPurge, "Also remove my &settings for "+info.Name, win.Rect{X: 20, Y: 176, W: 463, H: 24}, win.Face)
	ui.add(Confirm, ui.purge)
	if len(ui.u.Marker.Purge) == 0 {
		ui.purge.Enable(false)
		ui.purge.SetText(info.Name + " keeps no settings of its own")
	}

	ui.header(Removing, "Uninstalling", fmt.Sprintf("Please wait while %s is removed.", info.Name))
	ui.status = w.Label("Removing "+info.Name+"…", win.Rect{X: 20, Y: 76, W: 463, H: 20}, win.Normal, win.Face)
	ui.bar = w.Progress(win.Rect{X: 20, Y: 102, W: 463, H: 18})
	ui.detail = w.PathLabel("", win.Rect{X: 20, Y: 128, W: 463, H: 20}, win.Face)
	ui.add(Removing, ui.status, ui.bar, ui.detail)

	ui.doneTitle, ui.doneBody = ui.exterior(Done, "", "")
	ui.buttons()
}

// command handles a WM_COMMAND: only Next (IDOK) and Cancel (IDCANCEL) matter here, as the purge box
// is read once, when the removal starts. code, the notification code, is not needed.
func (ui *uninstallUI) command(id, code int) {
	switch id {
	case win.IDOK:
		ui.nextPressed()
	case win.IDCANCEL:
		ui.cancelPressed()
	}
}

// nextPressed handles Next: Uninstall on Confirm starts the removal, Close on Done closes the window.
// Nothing happens while Next is disabled (it is during the removal).
func (ui *uninstallUI) nextPressed() {
	if !ui.next.Enabled() {
		return
	}
	switch {
	case ui.flow.Last():
		ui.w.Close()
	case ui.flow.BeforeWork():
		ui.flow.Next()
		ui.show()
		ui.start()
	}
}

// cancelPressed handles Cancel and the close box: on Done it closes, during the removal it does
// nothing, and on Confirm it closes with app.ExitCancelled without asking (nothing has changed yet).
func (ui *uninstallUI) cancelPressed() {
	switch {
	case ui.flow.Last():
		ui.w.Close()
	case ui.flow.Working(): // removal is not undone half-way: let it finish
	default:
		ui.code = app.ExitCancelled
		ui.w.Close()
	}
}

// start runs the removal on a worker goroutine; wake shows its progress and its end.
//
// Steps:
//  1. Disable Cancel and reset the bar (0 to 1000, in thousandths).
//  2. Read the purge box on the UI thread, before the worker starts.
//  3. On the worker: wait for hold under -smoke, run Remove with a progress callback that stores the
//     counts and wakes the UI thread, then store the outcome and wake it once more.
func (ui *uninstallUI) start() {
	ui.cancel.Enable(false)
	ui.bar.SetProgress(0, 1000)
	purge := ui.purge.Enabled() && ui.purge.Checked() // a disabled box never purges, ticked or not
	go func() {
		if ui.hold != nil {
			<-ui.hold
		}
		res, err := ui.u.Remove(purge, func(done, total int64, file string) {
			ui.mu.Lock()
			ui.done, ui.total, ui.file = done, total, file
			ui.mu.Unlock()
			ui.w.Wake()
		})
		ui.mu.Lock()
		ui.res, ui.err, ui.finished = res, err, true
		ui.mu.Unlock()
		ui.w.Wake()
	}()
}

// wake runs on the UI thread after the worker calls Window.Wake: it shows the progress or, once the
// removal has ended, its outcome on the Done page.
//
// Steps:
//  1. Copy the worker's fields under mu.
//  2. Still running: move the bar and show the current entry.
//  3. Ended: set the exit code and fill Done, with the error and a hint to run it again, or with the
//     files kept because they changed after setup installed them.
func (ui *uninstallUI) wake() {
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
	if ui.flow.Page() != Removing { // the outcome was shown already; a late wake-up changes nothing
		return
	}
	ui.code = exitCode(err)
	ui.bar.SetProgress(1000, 1000)
	info := ui.u.Info
	if err != nil {
		ui.out.Printf("%v", err)
		ui.doneTitle.SetText("Uninstall did not finish")
		ui.doneBody.SetText(err.Error() + "\n\nYou can run it again from Apps & features.")
	} else {
		ui.out.Printf("Removed %s %s.", info.Name, info.Version)
		ui.doneTitle.SetText(info.Name + " was removed")
		text := fmt.Sprintf("%s %s is no longer on your computer.", info.Name, info.Version)
		if len(res.Kept) > 0 {
			text += fmt.Sprintf("\n\nKept, because they changed after setup installed them: %s (in %s).", strings.Join(res.Kept, ", "), filepath.Base(ui.u.Dir))
		}
		ui.doneBody.SetText(text)
	}
	ui.flow.Next()
	ui.show()
}

// smoke walks the three pages as a user would, capturing each, with the purge box left unticked; the
// removal runs for real (so pass the -root the install used).
//
// file is the JSON report; the captures go beside it. The capture names ("confirm", "removing",
// "done") are what TestWizardWindows expects. The step limit of 10 keeps a page that never moves on
// from hanging the walk. An error writing the report is not returned: the caller reads ui.code, and
// the test notices a missing report.
func (ui *uninstallUI) smoke(file string) {
	s := win.NewSmoke(ui.w, file, "uninstall", ui.title)
	ui.hold = make(chan struct{})
	ui.w.Show()
	for steps := 0; !ui.w.Closed() && steps < 10; steps++ {
		switch page := ui.flow.Page(); page {
		case Removing:
			s.Capture("removing", ui.flags())
			close(ui.hold)
			s.WaitFor(func() bool { return ui.flow.Page() != Removing })
		default:
			s.Capture(page.String(), ui.flags())
			ui.next.Click()
		}
	}
	ui.w.Close()
	s.Write(ui.code)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=cbf86043298d980736e7aa995942121ef3302d0fbbb70144c45187353b30b23d ====
