// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/app/app_test.go
// About: Tests for the identity constants: the shapes other code and tools rely on.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Tests for the identity constants: the shapes other code and tools rely on.

package app

import (
	"regexp"
	"strings"
	"testing"
)

// TestIdentity pins the x.y.z version shape (setups compare it, the dev console bumps it), the
// Title's company prefix and Name+Version suffix, and the CC BY credit the icon licence requires.
func TestIdentity(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Fatalf("version %q", Version)
	}
	if !strings.HasSuffix(Title, Name+" "+Version) || !strings.HasPrefix(Title, "Generic Company Inc. — ") {
		t.Fatalf("title %q", Title)
	}
	if !strings.Contains(Credit, "CC BY 2.5") {
		t.Fatal("icon credit missing")
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=cb07a1fe74d2b07b8e18822ba5e34dc412f2149005e937bc2cb3b004841361c7 ====
