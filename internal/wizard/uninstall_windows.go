// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/wizard/uninstall_windows.go
// About: Part of package wizard in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"acme/wizard/internal/app"
	"acme/wizard/internal/install"
	"acme/wizard/internal/sys"
	"acme/wizard/internal/win"
)

// uninstallUI is the uninstaller's window: Confirm, Removing, Done.
type uninstallUI struct {
	*frame
	u   *Uninstaller
	out *sys.Output

	purge               *win.Control
	status, bar, detail *win.Control
	doneTitle, doneBody *win.Control

	mu       sync.Mutex
	done     int64
	total    int64
	file     string
	finished bool
	res      install.Result
	err      error
	hold     chan struct{}
	code     int
}

func uninstallWindow(u *Uninstaller, o uninstallOptions, out *sys.Output, release func()) int {
	ico, _ := os.ReadFile(filepath.Join(u.Dir, install.IconName))
	f, err := newFrame(u.Info.Name+" Uninstall", UninstallFlow(), ico)
	if err != nil {
		release()
		out.Printf("%v", err)
		out.Done(sys.Error)
		return app.ExitFailed
	}
	out.SetQuiet(true)
	ui := &uninstallUI{frame: f, u: u, out: out, code: app.ExitCancelled}
	ui.build()
	f.w.OnCommand = ui.command
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

func (ui *uninstallUI) build() {
	w, info := ui.w, ui.u.Info
	ui.header(Confirm, "Uninstall "+info.Name, fmt.Sprintf("%s %s will be removed from your computer.", info.Name, info.Version))
	ui.add(Confirm, w.Label("This removes what setup installed, its shortcuts and its entry in Apps & features. "+
		"Files you changed or added are kept, and so is any data the ACME programs share.",
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

func (ui *uninstallUI) command(id, code int) {
	switch id {
	case win.IDOK:
		ui.nextPressed()
	case win.IDCANCEL:
		ui.cancelPressed()
	}
}

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

func (ui *uninstallUI) start() {
	ui.cancel.Enable(false)
	ui.bar.SetProgress(0, 1000)
	purge := ui.purge.Enabled() && ui.purge.Checked()
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
	if ui.flow.Page() != Removing {
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=235105db1a191bbedc34e7a9522b5ba81d0af14ba693461583b0f0285c6ac1e2 ====
