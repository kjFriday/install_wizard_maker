// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/maker/window_build_windows.go
// About: The maker window's Build: the build runs on a worker goroutine that hands its log lines...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The maker window's Build: the build runs on a worker goroutine that hands its log lines and result
// over under makerUI.mu and calls Window.Wake; wake shows them on the UI thread. Also the -smoke walk,
// which opens the window, builds, and records both.

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

// wake runs on the UI thread after the worker called Wake: it adds the new log lines and, once the build
// is done, its outcome.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=cc2f53863803be16edc054c128dcb0623d98a4bc886ce38cf3063d2689baa7bb ====
