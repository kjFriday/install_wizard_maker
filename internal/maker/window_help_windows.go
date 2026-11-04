// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/maker/window_help_windows.go
// About: The maker window's Help menu: About, and installing or uninstalling the maker itself...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The maker window's Help menu: About, and installing or uninstalling the maker itself (each in a window
// of its own; self.go does the work).

package maker

import (
	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/win"
)

// about is the About box's text.
func about() string {
	return app.Title + "\n\nMakes install wizards: one setup.exe per program, which installs it for the current user, " +
		"lists it in Apps & features and leaves an uninstaller.\n\n" + app.Credit +
		"\n\nCode: GNU AGPL-3.0. Generic Company Inc. is fictional."
}

// helpItems are the Help menu's entries.
func helpItems() []win.MenuItem {
	return []win.MenuItem{{ID: idInstallSelf, Text: "&Install for this user…"}, {ID: idUninstallSelf, Text: "&Uninstall…"}, {},
		{ID: idAbout, Text: "&About"}}
}

// helpCommand handles the Help menu's install entries: each starts its own window. Uninstalling the
// copy this window runs from closes the window (asking about unsaved changes first), so the uninstaller
// can remove its exe.
func (ui *makerUI) helpCommand(id int) {
	var err error
	switch id {
	case idInstallSelf:
		err = startSelfSetup(ui.self)
	case idUninstallSelf:
		var exe string
		var inside bool
		if exe, inside, err = selfUninstaller(ui.self, ""); err != nil || (inside && !ui.closing()) {
			break
		}
		if err = startUninstaller(exe); err == nil && inside {
			ui.w.Close()
		}
	}
	if err != nil {
		ui.w.MessageBox(app.Name, err.Error(), win.MBIconWarning)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=978198454b9049428157cff627caa4b500d294b3513624cb6a4ac22dc468a78a ====
