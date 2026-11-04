// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/winres/winres_test.go
// About: Tests for the pure-Go encoders: resource tree, .ico files, icon groups, version info.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Tests for the pure-Go encoders: resource tree, .ico files, icon groups, version info.
//
// They need no Windows API; pe_windows_test.go checks the same bytes against Windows. Two read
// ../../icon/app.ico, the maker's own icon (16, 32, 48 and a 256 px PNG).

package winres

import (
	"bytes"
	"os"
	"reflect"
	"sort"
	"testing"
)

// sample covers every shape of key: numbered types and ids, two languages of one id, a named type
// with a named resource, a numbered type with a named resource, and empty data.
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

// canon is a sorted copy of r with nil data made empty, so two lists compare equal whatever their order.
//
// The sort keys join a name with string(rune(number)): any total order will do here, it need not be
// the directory order.
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

// TestTreeRoundTrip checks that Decode(Encode(x)) gives x back, that the input order does not change
// the bytes, and that a section cut short is an error, not a panic.
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

// TestIcoAndGroup checks the .ico round trip byte for byte on the real icon, the group's 14-byte
// entries, rebuilding the .ico from the resources, and that damaged input or a missing group fails.
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

// TestVersionInfo checks that version info is deterministic and reads back (version and all four
// strings), and that ParseVersion keeps a full four-part Windows version.
func TestVersionInfo(t *testing.T) {
	v := VersionInfo{Version: ParseVersion("1.2.3"), Strings: [][2]string{
		{"CompanyName", "Generic Company Inc. (fictional)"}, {"FileDescription", "Generic Company Hello Setup"},
		{"ProductName", "Generic Company Hello"}, {"ProductVersion", "1.2.3"}}}
	b := v.Bytes()
	if len(b)%4 != 0 && len(b)%2 != 0 { // in effect only checks the length is even
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=f6a150a776c0b68bc83f482207afedf44fb6772ea7b7ce9137ce6a65fed5b22d ====
