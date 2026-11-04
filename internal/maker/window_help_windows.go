// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/maker/window_help_windows.go
// About: The maker window's Help menu: About, and installing or uninstalling the maker itself.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// The maker window's Help menu: About, and installing or uninstalling the maker itself.
//
// Install and uninstall each run in a window of their own (the setup or uninstall wizard); self.go
// does the work. window_windows.go builds the menu from helpItems and passes every command id it
// does not handle itself to helpCommand.

package maker

import (
	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/win"
)

// about is the About box's text: the title with version, what the maker does, the icon credit, and
// the license.
func about() string {
	return app.Title + "\n\nMakes install wizards: one setup.exe per program, which installs it for the current user, " +
		"lists it in Apps & features and leaves an uninstaller.\n\n" + app.Credit +
		"\n\nCode: GNU AGPL-3.0. Generic Company Inc. is fictional."
}

// helpItems are the Help menu's entries; the empty item is a separator and "&" marks the access key.
func helpItems() []win.MenuItem {
	return []win.MenuItem{{ID: idInstallSelf, Text: "&Install for this user…"}, {ID: idUninstallSelf, Text: "&Uninstall…"}, {},
		{ID: idAbout, Text: "&About"}}
}

// helpCommand handles the Help menu's install entries: each starts its own window. Uninstalling the
// copy this window runs from closes the window (asking about unsaved changes first), so the uninstaller
// can remove its exe.
//
// Ids other than the two install entries are ignored. Errors (not installed, cannot build or start)
// show in a warning box; the window stays open then.
//
// Uninstall steps:
//  1. Find the installed uninstall.exe and whether this window runs from that install.
//  2. If it does, ask closing (which may refuse during a build or on cancelled unsaved changes).
//  3. Start the uninstaller and, when inside, close the window so the exe is free to delete.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=8374623f16a537717bb91c8c37fa12d694491b307a69584d7209da18b3a6f51b ====
