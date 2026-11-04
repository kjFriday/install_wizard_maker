// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/wizard/flow.go
// About: The wizard's page order and button rules, kept free of windows so tests can walk them.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The wizard's page order and button rules, kept free of windows so tests can walk them.
//
// Both windows (setup_windows.go and uninstall_windows.go, through frame_windows.go) ask a Flow
// which page comes next, whether Back and Next are enabled, and what Next says. Each flow has one
// work page (Progress or Removing): the pages before it gather choices, the work runs on it with
// Next and Back off, and from it on there is no going back.
//
// Types: Page (one page), Flow (the order and the cursor), Gate (what the current page waits for).

package wizard

// Page is one page of the install wizard or the uninstaller.
type Page int

// The pages: the install wizard's first, then the uninstaller's. pageNames follows this order.
const (
	Welcome   Page = iota // setup: what will be installed, or why it cannot be
	License               // setup: the license to accept
	Folder                // setup: where to install
	Shortcuts             // setup: Start-menu and desktop shortcuts
	Progress              // setup: working
	Finish                // setup: what happened, and Launch
	Confirm               // uninstaller: what will go
	Removing              // uninstaller: working
	Done                  // uninstaller: what happened
)

// pageNames are the pages' names, index-aligned with the Page constants. They are also the -smoke
// capture names that main_test.go expects, so they must not change.
var pageNames = [...]string{"welcome", "license", "folder", "shortcuts", "progress", "finish", "confirm", "removing", "done"}

// String is the page's lower-case name ("welcome" ...).
func (p Page) String() string { return pageNames[p] }

// Exterior pages (welcome, finish and the uninstaller's done) have the side banner; the others a
// white header.
func (p Page) Exterior() bool { return p == Welcome || p == Finish || p == Done }

// Flow is the order of pages and what Back and Next may do. It knows nothing about windows.
type Flow struct {
	pages []Page
	at    int    // the index of the current page in pages
	work  Page   // the page the work runs on
	verb  string // Next's label when it starts the work ("&Install")
	close string // Next's label on the last page ("&Finish")
}

// SetupFlow: Welcome, License (when there is one), Folder (when asked, but not for an upgrade, which
// keeps its folder), Shortcuts (when asked), Progress, Finish.
func SetupFlow(license, askFolder, askShortcuts, upgrade bool) *Flow {
	pages := []Page{Welcome}
	if license {
		pages = append(pages, License)
	}
	if askFolder && !upgrade {
		pages = append(pages, Folder)
	}
	if askShortcuts {
		pages = append(pages, Shortcuts)
	}
	return &Flow{pages: append(pages, Progress, Finish), work: Progress, verb: "&Install", close: "&Finish"}
}

// UninstallFlow: Confirm, Removing, Done.
func UninstallFlow() *Flow {
	return &Flow{pages: []Page{Confirm, Removing, Done}, work: Removing, verb: "&Uninstall", close: "&Close"}
}

// Page is the current page.
func (f *Flow) Page() Page { return f.pages[f.at] }

// Pages are all the pages, in order.
func (f *Flow) Pages() []Page { return f.pages }

// Next moves on a page; false when already on the last.
func (f *Flow) Next() bool {
	if f.at == len(f.pages)-1 {
		return false
	}
	f.at++
	return true
}

// Back moves back a page, when CanBack.
func (f *Flow) Back() bool {
	if !f.CanBack() {
		return false
	}
	f.at--
	return true
}

// CanBack: not from the first page, nor during or after the work.
func (f *Flow) CanBack() bool { return f.at > 0 && f.at < f.index(f.work) }

// Working reports whether this is the page the work runs on.
func (f *Flow) Working() bool { return f.Page() == f.work }

// Last reports whether this is the last page.
func (f *Flow) Last() bool { return f.at == len(f.pages)-1 }

// BeforeWork reports whether Next starts the work.
func (f *Flow) BeforeWork() bool { return f.at+1 == f.index(f.work) }

// NextLabel is the Next button's text: "Install" (or "Uninstall") when it starts the work, "Finish"
// (or "Close") on the last page.
func (f *Flow) NextLabel() string {
	switch {
	case f.Last():
		return f.close
	case f.BeforeWork():
		return f.verb
	}
	return "&Next >"
}

// Gate is what Next waits for.
type Gate struct {
	Accepted bool // the license is accepted
	Blocked  bool // setup cannot go on (the reason is on the welcome page)
}

// CanNext: never during the work; the welcome page unless blocked; the license page once accepted.
//
// The uninstaller's Confirm page follows the welcome page's rule. Every other page may go on.
func (f *Flow) CanNext(g Gate) bool {
	switch f.Page() {
	case Welcome, Confirm:
		return !g.Blocked
	case License:
		return g.Accepted
	case f.work: // not a constant: Progress or Removing, whichever this flow works on
		return false
	}
	return true
}

// index is p's position in the flow, or -1 when the flow does not have it.
func (f *Flow) index(p Page) int {
	for i, q := range f.pages {
		if q == p {
			return i
		}
	}
	return -1
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=56e2a52493bd2fc21ab3a9b4a1a72a724b54cddb2aaacf896e91b8b4ee53d7c5 ====
