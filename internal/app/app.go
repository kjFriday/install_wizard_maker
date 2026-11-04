// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/app/app.go
// About: Package app holds the wizard maker's identity and the exit codes every role shares.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package app holds the wizard maker's identity and the exit codes every role shares.
//
// One exe plays three roles (the maker, every setup.exe it builds, and every uninstall.exe those
// setups leave), and all three import this package for their names and exit codes. It has no code,
// only constants:
//   - the identity block: Version, ID, Name, Title, Publisher, Group, ExeName and Credit. The maker
//     installs itself with these (maker.SelfSpec), and setup.json at the module root must say the same
//     (TestOwnSetupJSON).
//   - the exit codes 0 to 4, which README documents for scripts that run the maker, a setup or an
//     uninstaller.
//
// The portfolio's dev console bumps the version by rewriting the first line of this file that starts
// with "Version" and an "=": keep the Version line the first of that shape.
package app

// The wizard maker's identity: what windows, Apps & features and the maker's own install show.
const (
	// Version is the maker's release, dotted numeric (TestIdentity checks the x.y.z shape). It is
	// stamped into every setup.exe's manifest as part of the maker's name.
	Version = "0.1.0"
	// ID is the app id the maker installs itself under: its Uninstall key name and install.json app_id.
	ID = "GenericCompany.Wizard"
	// Name is the display name: message-box captions, -version output, and the Start-menu shortcut
	// and install folder names of the maker's own install.
	Name = "Generic Company Wizard Maker"
	// Title is the maker window's title bar text (the open file's name is appended) and the About
	// box's first line.
	Title = "Generic Company Inc. — " + Name + " " + Version
	// Publisher is the Apps & features publisher.
	Publisher = "Generic Company Inc. (fictional)"
	Group     = "Generic Company Inc" // Start-menu and Programs folder, as the other Generic Company programs use
	// ExeName is the maker's file name, in the install folder and in usage text.
	ExeName = "wizard-maker.exe"
	// Credit is the icon's attribution, which CC BY 2.5 requires; the About box shows it.
	Credit = "Icon: Silk by Mark James (famfamfam.com/lab/icons/silk), CC BY 2.5 " +
		"(creativecommons.org/licenses/by/2.5), enlarged by nearest-neighbour."
)

// Exit codes of the maker, the setup wizard and the uninstaller. README lists them for scripts, so
// their numbers do not change.
const (
	ExitOK        = 0 // done
	ExitFailed    = 1 // failed; an install is rolled back
	ExitUsage     = 2 // bad arguments or setup.json
	ExitCancelled = 3 // the user cancelled
	ExitRefused   = 4 // refused: downgrade, foreign folder, file in use, elevated, Python missing
)

// ==== stamp v1 | author=kjFriday | hmac-sha256=9a9adaa9369cbe57b3ae03e7f232bc8e141fa3b886f64dc0f378ce8becadfff6 ====
