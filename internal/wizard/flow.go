// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/wizard/flow.go
// About: Part of package wizard in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package wizard

// Page is one page of the install wizard or the uninstaller.
type Page int

const (
	Welcome Page = iota
	License
	Folder
	Shortcuts
	Progress
	Finish
	Confirm  // uninstaller: what will go
	Removing // uninstaller: working
	Done     // uninstaller: what happened
)

var pageNames = [...]string{"welcome", "license", "folder", "shortcuts", "progress", "finish", "confirm", "removing", "done"}

func (p Page) String() string { return pageNames[p] }

// Exterior pages (welcome and finish) have the side banner; the others a white header.
func (p Page) Exterior() bool { return p == Welcome || p == Finish || p == Done }

// Flow is the order of pages and what Back and Next may do. It knows nothing about windows.
type Flow struct {
	pages []Page
	at    int
	work  Page // the page the work runs on
	verb  string
	close string
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
func (f *Flow) CanNext(g Gate) bool {
	switch f.Page() {
	case Welcome, Confirm:
		return !g.Blocked
	case License:
		return g.Accepted
	case f.work:
		return false
	}
	return true
}

func (f *Flow) index(p Page) int {
	for i, q := range f.pages {
		if q == p {
			return i
		}
	}
	return -1
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=d107a294f9ca48345a7a7db683bf7436e841db3454e381768489de21a900a2af ====
