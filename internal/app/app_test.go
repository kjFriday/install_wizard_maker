// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/app/app_test.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/app/app_test.go
// About: Tests for package app.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package app

import (
	"regexp"
	"strings"
	"testing"
)

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

// ==== stamp v1 | author=kjFriday | hmac-sha256=eab5a7dcb34e904a925c5db148489368a4fb5656eb78c42abcff47c7c3f23217 ====
