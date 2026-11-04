// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/maker/window_build_windows.go
// About: The maker window's Build command, run on a worker goroutine, and the -smoke walk.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The maker window's Build command, run on a worker goroutine, and the -smoke walk.
//
// The build runs on a worker goroutine that hands its log lines and result over under makerUI.mu and
// calls Window.Wake; wake shows them on the UI thread. Win32 controls belong to the thread that made
// them, so the worker never touches a control itself. Also the -smoke walk, which opens the window,
// builds, and records both.
//
// Flow of one build:
//  1. command (window_windows.go) routes Build > Build setup.exe, F5 or the button to startBuild.
//  2. startBuild saves if needed, disables the button and starts the worker.
//  3. The worker calls Build; each log line is queued in pending and a Wake is posted.
//  4. wake, on the UI thread, appends the queued lines to the log and, once done, the outcome.

package maker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/win"
)

// startBuild saves if needed (a build is always of setup.json as saved), then builds on a worker.
//
// It runs on the UI thread. It does nothing while a build runs, or when the save is cancelled or
// fails. The stub is the maker's own exe (ui.self), read here so a read error shows at once in a
// message box rather than in the log.
//
// Steps:
//  1. Save when there are unsaved changes or the spec has never been saved.
//  2. Read the stub.
//  3. Clear the log, mark the build running, disable the Build button and reset the shared state.
//  4. Start the worker: it calls Build with a log function that queues each line under mu and wakes
//     the UI thread, then stores the result and wakes it once more.
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
	// the worker gets the spec as just saved (save loads it into ui.s).
	// Known limitation: it is the same *spec.Spec the window keeps; Add file, Add folder and Remove
	// during a build assign ui.s.Files while the worker may be reading it.
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

// wake runs on the UI thread after the worker called Wake: it adds the new log lines and, once the build
// is done, its outcome.
//
// The shared state is copied out under mu and the lock is dropped before any control is touched,
// so the worker never waits on the UI. Wake-ups merge, so one call may bring many lines. The
// outcome is one of three: setup.json has problems (a *SpecError, shown indented under the file's
// name), the build failed for another reason, or it succeeded and becomes the setup.exe that
// Build > Run the last setup.exe opens.
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
			ui.logText.WriteString(fmt.Sprintf("Done: %s in %s. Build > Run the last setup.exe tries it.\n", count(res.Files, "file"), filepath.Base(res.Path)))
		}
	}
	ui.log.SetText(ui.logText.String())
}

// smoke shows the window with its setup.json, builds it, and records both.
//
// file is the report path given to -smoke; the screenshots go beside it (see win.NewSmoke). The
// result is app.ExitOK when a setup.exe was built and app.ExitFailed when not, or when the report
// cannot be written.
//
// Steps:
//  1. Show the window and capture it as "form".
//  2. Start the build and pump messages until the UI thread has shown its outcome (WaitFor gives up
//     after a minute).
//  3. Capture the window as "built", with a flag saying whether a setup.exe came out.
//  4. Close the window and write the report with the exit code.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=824e2043c4fbce21e43b5aa93393c9719512a76ba551c52ec09594db1ef36319 ====
