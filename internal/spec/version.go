// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/spec/version.go
// About: Version comparison: dotted numeric versions, part by part.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Version comparison: dotted numeric versions, part by part.

package spec

import "strings"

// atoi reads one version part, which versionPattern keeps to digits. It checks nothing itself: a part
// that is not all digits (another installer's marker may say "0.2.0-beta") gives a meaningless number,
// and it returns as soon as the number passes 1<<20.
func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
		if n > 1<<20 {
			return n
		}
	}
	return n
}

// CompareVersions compares dotted numeric versions ("1.10" > "1.9"; missing parts count as 0).
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = atoi(pa[i])
		}
		if i < len(pb) {
			y = atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=a74f46310a4144d8d3b31dd2f9e261310698cf6ce0a65d86ff0fe08b4ef42105 ====
