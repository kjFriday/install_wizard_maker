// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/app/app.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/app/app.go
// About: Package app is the wizard maker's identity (name, id, version, credit) and the exit codes...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package app is the wizard maker's identity (name, id, version, credit) and the exit codes every role
// shares.
package app

const (
	Version   = "0.1.0"
	ID        = "GenericCompany.Wizard"
	Name      = "Generic Company Wizard Maker"
	Title     = "Generic Company Inc. — " + Name + " " + Version
	Publisher = "Generic Company Inc. (fictional)"
	Group     = "Generic Company Inc" // Start-menu and Programs folder, as the other Generic Company programs use
	ExeName   = "wizard-maker.exe"
	Credit    = "Icon: Silk by Mark James (famfamfam.com/lab/icons/silk), CC BY 2.5 " +
		"(creativecommons.org/licenses/by/2.5), enlarged by nearest-neighbour."
)

// Exit codes of the maker, the setup wizard and the uninstaller.
const (
	ExitOK        = 0 // done
	ExitFailed    = 1 // failed; an install is rolled back
	ExitUsage     = 2 // bad arguments or setup.json
	ExitCancelled = 3 // the user cancelled
	ExitRefused   = 4 // refused: downgrade, foreign folder, file in use, elevated, Python missing
)

// ==== stamp v1 | author=kjFriday | hmac-sha256=4c5f900e390213b04a860fecd5a449fb1492a1839ae986ec2a1ed5a9c9c13a04 ====
