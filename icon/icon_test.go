// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: icon/icon_test.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: icon/icon_test.go
// About: Tests for package icon.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package icon

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image/png"
	"os"
	"regexp"
	"testing"
)

// Every size is the 16 px Silk original enlarged by whole pixels, and the files match the credits table.
func TestPixelExactAndCredited(t *testing.T) {
	raw, err := os.ReadFile("silk-wand.png")
	if err != nil {
		t.Fatal(err)
	}
	small, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for k, name := range map[int]string{2: "app-32.png", 3: "app-48.png", 16: "app-256.png"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		big, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if big.Bounds().Dx() != 16*k {
			t.Fatalf("x%d: %v", k, big.Bounds())
		}
		for y := 0; y < 16*k; y++ {
			for x := 0; x < 16*k; x++ {
				r1, g1, b1, a1 := big.At(x, y).RGBA()
				r2, g2, b2, a2 := small.At(x/k, y/k).RGBA()
				if a1 != a2 || (a1 != 0 && (r1 != r2 || g1 != g2 || b1 != b2)) {
					t.Fatalf("x%d pixel (%d,%d) differs", k, x, y)
				}
			}
		}
	}
	if binary.LittleEndian.Uint16(ICO[4:]) != 4 {
		t.Fatal("app.ico should hold 16, 32, 48 and 256 px")
	}
	readme, _ := os.ReadFile("README.md")
	if !bytes.Contains(readme, []byte("Creative Commons Attribution 2.5")) {
		t.Fatal("credit missing")
	}
	rows := regexp.MustCompile("\\| `([^`]+)` \\|[^|]*\\| `([0-9a-f]{64})` \\|").FindAllSubmatch(readme, -1)
	if len(rows) != 6 {
		t.Fatalf("%d files listed", len(rows))
	}
	for _, r := range rows {
		b, err := os.ReadFile(string(r[1]))
		sum := sha256.Sum256(b)
		if err != nil || hex.EncodeToString(sum[:]) != string(r[2]) {
			t.Fatalf("%s does not match its SHA-256", r[1])
		}
	}
	if !bytes.Contains(readme, []byte("Common Controls 6")) {
		t.Fatal("app.res should carry the Common Controls 6 manifest")
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=e326fba3eb417bb269681be749cfa9f15eb7b61c4f4d2c2c9d56abc3b5dc9ead ====
