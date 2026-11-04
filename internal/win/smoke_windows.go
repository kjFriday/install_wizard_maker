// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/win/smoke_windows.go
// About: The -smoke harness: walks a window's steps, writes a JSON report and PNG captures.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The -smoke harness: walks a window's steps, writes a JSON report and PNG captures.
//
// Every role (the maker, setup.exe and uninstall.exe) takes a -smoke FILE flag. Instead of waiting
// for a person, the role drives its own window page by page and records what a user would have seen:
// a PNG of each step and the position, text and state of every control (Window.Info). main_test.go
// reads the report back (its smokeReport and checkSmoke) and checks that every control fits its text
// and sits inside the client area, so the JSON tags here are a contract with that test.
//
// The flow, as the roles use it:
//  1. NewSmoke records the window's DPI, text size, font, icon and client size.
//  2. For each step the role clicks or waits (WaitFor), then calls Capture, which settles the window,
//     saves <report base>-NN-<step>.png beside the report and appends a SmokePage.
//  3. Write adds the exit code and any recovered handler panic and writes the report.
//
// Types: SmokeReport (the whole report), SmokePage (one step), Smoke (the running walk).

package win

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SmokeReport is what a -smoke run writes: every step as the user saw it, captured, with its controls.
//
// The JSON names are read by main_test.go; renaming a tag breaks the end-to-end tests.
type SmokeReport struct {
	Role      string      `json:"role"`      // "maker", "setup" or "uninstall"
	Title     string      `json:"title"`     // the window's title
	DPI       uint32      `json:"dpi"`       // the window's DPI when the walk started (96 = 100 %)
	TextScale float64     `json:"textScale"` // the user's text size (1 = 100 %)
	Font      string      `json:"font"`      // the face name of the window's fonts
	IconSet   bool        `json:"iconSet"`   // the window class has the exe's icon
	Client    [2]int      `json:"client"`    // client width and height in pixels
	Pages     []SmokePage `json:"pages"`
	Exit      int         `json:"exit"`            // the role's exit code (app.Exit*)
	Panic     string      `json:"panic,omitempty"` // the first panic a handler raised, with its stack
}

// SmokePage is one captured step.
//
// PNG is the capture's file name, relative to the report's folder; it is empty when Error says why
// there is no capture.
type SmokePage struct {
	Name     string          `json:"name"`
	PNG      string          `json:"png"`
	Flags    map[string]bool `json:"flags,omitempty"` // e.g. whether Next was enabled
	Controls []ControlInfo   `json:"controls"`
	Error    string          `json:"error,omitempty"` // why there is no PNG
}

// Smoke drives a window through its steps and records them (the -smoke flag every role has).
//
// Like the window it drives, a Smoke is used on the UI thread only.
type Smoke struct {
	w      *Window
	file   string // the report's path
	Report SmokeReport
}

// NewSmoke starts a report written to file; captures go beside it as <file>-NN-<step>.png.
//
// It creates the report's folder and records the window's DPI, text size, font face, icon and client
// size as they are now. Nothing is written until Capture or Write.
func NewSmoke(w *Window, file, role, title string) *Smoke {
	os.MkdirAll(filepath.Dir(file), 0o755) // an error shows up later, when Write cannot create the file
	cw, ch := w.ClientSize()
	return &Smoke{w: w, file: file, Report: SmokeReport{Role: role, Title: title, DPI: w.DPI(), TextScale: w.TextScale(),
		Font: w.FontFace(), IconSet: w.IconSet(), Client: [2]int{cw, ch}}}
}

// Settle lets the window handle everything pending, paint, and finish the buttons' fade animations.
//
// It blocks for about 300 ms: 20 rounds of Pump and a 15 ms sleep. The count is a timing guess, long
// enough for the button fades to end so two captures of the same step do not differ by timing.
func (s *Smoke) Settle() {
	for i := 0; i < 20; i++ {
		s.w.Pump()
		time.Sleep(15 * time.Millisecond)
	}
}

// Capture saves the window as a PNG and records its controls under name.
//
// flags are extra facts the role wants in the report (the frame's "next" and "back", the maker's
// "built"); nil records none. The PNG goes beside the report as <report base>-NN-<name>.png, where NN
// is the page's 1-based number. A capture that fails is not an error: the page records why instead.
//
// Steps:
//  1. Settle, so the window has painted.
//  2. Record the controls (Window.Info) as they are after settling.
//  3. Capture, retrying up to four times after another Settle.
//  4. Encode the PNG, or record the error in place of its name.
func (s *Smoke) Capture(name string, flags map[string]bool) {
	s.Settle()
	base := strings.TrimSuffix(s.file, filepath.Ext(s.file))
	file := fmt.Sprintf("%s-%02d-%s.png", base, len(s.Report.Pages)+1, name)
	page := SmokePage{Name: name, PNG: filepath.Base(file), Flags: flags, Controls: s.w.Info()}
	img, err := s.w.Capture()
	for try := 0; err != nil && try < 4; try++ { // the very first capture can come too early
		s.Settle()
		img, err = s.w.Capture()
	}
	if err == nil {
		var out *os.File
		if out, err = os.Create(file); err == nil {
			err = png.Encode(out, img)
			out.Close()
		}
	}
	if err != nil {
		page.PNG, page.Error = "", err.Error()
	}
	s.Report.Pages = append(s.Report.Pages, page)
}

// WaitFor pumps messages until done reports true (or a minute passes, or the window closes).
//
// It polls every 20 ms, so done runs on the UI thread and may read UI state. Running out of time is
// not reported: the next Capture shows where the walk stopped.
func (s *Smoke) WaitFor(done func() bool) {
	for deadline := time.Now().Add(time.Minute); !done() && time.Now().Before(deadline) && !s.w.Closed(); {
		s.w.Pump()
		time.Sleep(20 * time.Millisecond)
	}
}

// Write writes the report.
//
// exit is the role's exit code; the first handler panic the window recovered (Window.Panic) is added
// too. The report is indented JSON and replaces any earlier file.
func (s *Smoke) Write(exit int) error {
	s.Report.Exit = exit
	if s.w.Panic != nil {
		s.Report.Panic = s.w.Panic.Error()
	}
	b, err := json.MarshalIndent(s.Report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.file, b, 0o644)
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=92176194d819e1b8567741191d20862210c43819bfd5d16577e7fcd0419e80f5 ====
