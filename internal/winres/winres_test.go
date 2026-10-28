// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/winres/winres_test.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/winres/winres_test.go
// About: Tests for package winres.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package winres

import (
	"bytes"
	"os"
	"reflect"
	"sort"
	"testing"
)

func sample() []Resource {
	return []Resource{
		{Type: TypeManifest, ID: 1, Lang: LangEnUS, Data: []byte("<assembly/>")},
		{Type: TypeIcon, ID: 2, Lang: LangEnUS, Data: []byte("second")},
		{Type: TypeIcon, ID: 1, Lang: LangEnUS, Data: []byte("first image")},
		{Type: TypeIcon, ID: 1, Lang: 0x0407, Data: []byte("german")},
		{TypeName: "CUSTOM", Name: "MAINDATA", Lang: 0, Data: []byte{1, 2, 3}},
		{Type: 10, Name: "Named", Lang: LangEnUS, Data: nil},
	}
}

func canon(r []Resource) []Resource {
	c := append([]Resource(nil), r...)
	for i := range c {
		if c[i].Data == nil {
			c[i].Data = []byte{}
		}
	}
	sort.Slice(c, func(i, j int) bool {
		a, b := c[i], c[j]
		if a.TypeName != b.TypeName || a.Type != b.Type {
			return a.TypeName+string(rune(a.Type)) < b.TypeName+string(rune(b.Type))
		}
		if a.Name != b.Name || a.ID != b.ID {
			return a.Name+string(rune(a.ID)) < b.Name+string(rune(b.ID))
		}
		return a.Lang < b.Lang
	})
	return c
}

func TestTreeRoundTrip(t *testing.T) {
	const rva = 0x5000
	sec := Encode(sample(), rva)
	got, err := Decode(sec, rva)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(canon(got), canon(sample())) {
		t.Fatalf("round trip:\n%v\n%v", canon(got), canon(sample()))
	}
	shuffled := sample()
	shuffled[0], shuffled[3] = shuffled[3], shuffled[0]
	if !bytes.Equal(Encode(shuffled, rva), sec) {
		t.Fatal("input order changed the bytes")
	}
	if _, err := Decode(sec[:20], rva); err == nil {
		t.Fatal("a cut-off section decoded")
	}
}

func TestIcoAndGroup(t *testing.T) {
	ico, err := os.ReadFile("../../icon/app.ico")
	if err != nil {
		t.Fatal(err)
	}
	images, err := ParseICO(ico)
	if err != nil {
		t.Fatal(err)
	}
	var sizes []int
	for _, im := range images {
		sizes = append(sizes, im.Width)
	}
	if !reflect.DeepEqual(sizes, []int{16, 32, 48, 256}) || !bytes.HasPrefix(images[3].Data, []byte("\x89PNG")) {
		t.Fatalf("images %v", sizes)
	}
	if !bytes.Equal(EncodeICO(images), ico) {
		t.Fatal("EncodeICO(ParseICO(x)) != x")
	}
	res := IconResources(images, 1)
	if len(res) != 5 || res[4].Type != TypeGroupIcon || len(res[4].Data) != 6+14*4 {
		t.Fatalf("resources %+v", res[4])
	}
	back, err := ICOFromResources(res)
	if err != nil || !bytes.Equal(back, ico) {
		t.Fatalf("icon group did not rebuild the .ico: %v", err)
	}
	for _, bad := range [][]byte{nil, []byte("not an icon"), ico[:30]} {
		if _, err := ParseICO(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if _, err := ICOFromResources(sample()); err == nil {
		t.Error("icons without a group rebuilt")
	}
}

func TestVersionInfo(t *testing.T) {
	v := VersionInfo{Version: ParseVersion("1.2.3"), Strings: [][2]string{
		{"CompanyName", "Generic Company Inc. (fictional)"}, {"FileDescription", "Generic Company Hello Setup"},
		{"ProductName", "Generic Company Hello"}, {"ProductVersion", "1.2.3"}}}
	b := v.Bytes()
	if len(b)%4 != 0 && len(b)%2 != 0 {
		t.Fatalf("odd length %d", len(b))
	}
	if !bytes.Equal(b, v.Bytes()) {
		t.Fatal("not deterministic")
	}
	got, strs, err := ReadVersionInfo(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != [4]uint16{1, 2, 3, 0} || strs["FileDescription"] != "Generic Company Hello Setup" || strs["CompanyName"] != "Generic Company Inc. (fictional)" || len(strs) != 4 {
		t.Fatalf("%v %v", got, strs)
	}
	if ParseVersion("10.0.19045.1") != [4]uint16{10, 0, 19045, 1} {
		t.Fatal("ParseVersion")
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=b61ee99f228aa9c0a3f8391a3dc53a6523e404d48a8cb6b3ff15719352789ea7 ====
