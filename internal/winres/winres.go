// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/winres/winres.go
// About: Package winres reads and writes Windows resources: .ico files, icon groups, version info...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package winres reads and writes Windows resources: .ico files, icon groups, version info, and the
// resource section of a PE file, all in pure Go so a setup.exe is the same bytes on every machine.
package winres

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"unicode/utf16"
)

// Resource types and the language every resource here is written in.
const (
	TypeIcon      = 3
	TypeGroupIcon = 14
	TypeVersion   = 16
	TypeManifest  = 24
	LangEnUS      = 0x0409
)

// Resource is one resource. A named type or name (rare) is kept in TypeName / Name instead of the number.
type Resource struct {
	Type     uint32
	TypeName string
	ID       uint32
	Name     string
	Lang     uint32
	Data     []byte
}

var le = binary.LittleEndian

// Decode reads a resource section's tree. rva is the section's address: data entries point by address.
func Decode(sec []byte, rva uint32) ([]Resource, error) {
	var out []Resource
	type level struct {
		num  uint32
		name string
	}
	var walk func(off uint32, depth int, path []level) error
	walk = func(off uint32, depth int, path []level) error {
		if depth > 2 || int(off)+16 > len(sec) {
			return errors.New("resource directory out of range")
		}
		n := int(le.Uint16(sec[off+12:])) + int(le.Uint16(sec[off+14:]))
		for i := 0; i < n; i++ {
			e := int(off) + 16 + 8*i
			if e+8 > len(sec) {
				return errors.New("resource entry out of range")
			}
			nameField, target := le.Uint32(sec[e:]), le.Uint32(sec[e+4:])
			var lv level
			if nameField&0x80000000 != 0 {
				s, err := dirString(sec, nameField&^0x80000000)
				if err != nil {
					return err
				}
				lv.name = s
			} else {
				lv.num = nameField
			}
			p := append(append([]level{}, path...), lv)
			if target&0x80000000 != 0 {
				if err := walk(target&^0x80000000, depth+1, p); err != nil {
					return err
				}
				continue
			}
			if depth != 2 || int(target)+16 > len(sec) {
				return errors.New("resource data entry out of place")
			}
			addr, size := le.Uint32(sec[target:]), le.Uint32(sec[target+4:])
			if addr < rva || uint64(addr-rva)+uint64(size) > uint64(len(sec)) {
				return errors.New("resource data out of range")
			}
			out = append(out, Resource{Type: p[0].num, TypeName: p[0].name, ID: p[1].num, Name: p[1].name,
				Lang: p[2].num, Data: append([]byte(nil), sec[addr-rva:addr-rva+size]...)})
		}
		return nil
	}
	if err := walk(0, 0, nil); err != nil {
		return nil, err
	}
	return out, nil
}

func dirString(sec []byte, off uint32) (string, error) {
	if int(off)+2 > len(sec) {
		return "", errors.New("resource name out of range")
	}
	n := int(le.Uint16(sec[off:]))
	if int(off)+2+2*n > len(sec) {
		return "", errors.New("resource name out of range")
	}
	u := make([]uint16, n)
	for i := range u {
		u[i] = le.Uint16(sec[int(off)+2+2*i:])
	}
	return string(utf16.Decode(u)), nil
}

type key struct {
	num  uint32
	name string
}

func less(a, b key) bool { // named entries first (by name), then numbers ascending
	if (a.name != "") != (b.name != "") {
		return a.name != ""
	}
	if a.name != "" {
		return a.name < b.name
	}
	return a.num < b.num
}

// Encode writes a resource section for address rva: directories, data entries, names, then the data,
// each blob 8-byte aligned. The order is fixed, so equal resources give equal bytes.
func Encode(res []Resource, rva uint32) []byte {
	type tree map[key]map[key]map[uint32][]byte
	t := tree{}
	for _, r := range res {
		tk, nk := key{r.Type, r.TypeName}, key{r.ID, r.Name}
		if t[tk] == nil {
			t[tk] = map[key]map[uint32][]byte{}
		}
		if t[tk][nk] == nil {
			t[tk][nk] = map[uint32][]byte{}
		}
		t[tk][nk][r.Lang] = r.Data
	}
	sorted := func(m map[key]map[uint32][]byte) []key {
		var ks []key
		for k := range m {
			ks = append(ks, k)
		}
		sort.Slice(ks, func(i, j int) bool { return less(ks[i], ks[j]) })
		return ks
	}
	var types []key
	for k := range t {
		types = append(types, k)
	}
	sort.Slice(types, func(i, j int) bool { return less(types[i], types[j]) })

	// sizes, to place every part before writing
	dirSize := 16 + 8*len(types)
	var names []string
	nData := 0
	for _, tk := range types {
		dirSize += 16 + 8*len(t[tk])
		if tk.name != "" {
			names = append(names, tk.name)
		}
		for _, nk := range sorted(t[tk]) {
			dirSize += 16 + 8*len(t[tk][nk])
			nData += len(t[tk][nk])
			if nk.name != "" {
				names = append(names, nk.name)
			}
		}
	}
	dataEntries := dirSize
	strOff := dataEntries + 16*nData
	strPos := map[string]int{}
	pos := strOff
	for _, n := range names {
		if _, ok := strPos[n]; !ok {
			strPos[n] = pos
			pos += 2 + 2*len(utf16.Encode([]rune(n)))
		}
	}
	blob := (pos + 7) &^ 7

	out := make([]byte, blob)
	dirPos, entryPos := 0, dataEntries
	var blobs bytes.Buffer
	writeDir := func(at int, ks []key) int { // returns where the entries start
		le.PutUint16(out[at+12:], uint16(countNamed(ks)))
		le.PutUint16(out[at+14:], uint16(len(ks)-countNamed(ks)))
		return at + 16
	}
	nameField := func(k key) uint32 {
		if k.name != "" {
			return 0x80000000 | uint32(strPos[k.name])
		}
		return k.num
	}
	root := dirPos
	dirPos += 16 + 8*len(types)
	e := writeDir(root, types)
	for _, tk := range types {
		ids := sorted(t[tk])
		sub := dirPos
		dirPos += 16 + 8*len(ids)
		le.PutUint32(out[e:], nameField(tk))
		le.PutUint32(out[e+4:], 0x80000000|uint32(sub))
		e += 8
		ie := writeDir(sub, ids)
		for _, nk := range ids {
			langs := make([]key, 0, len(t[tk][nk]))
			for l := range t[tk][nk] {
				langs = append(langs, key{num: l})
			}
			sort.Slice(langs, func(i, j int) bool { return langs[i].num < langs[j].num })
			lsub := dirPos
			dirPos += 16 + 8*len(langs)
			le.PutUint32(out[ie:], nameField(nk))
			le.PutUint32(out[ie+4:], 0x80000000|uint32(lsub))
			ie += 8
			le2 := writeDir(lsub, langs)
			for _, l := range langs {
				data := t[tk][nk][l.num]
				le.PutUint32(out[le2:], l.num)
				le.PutUint32(out[le2+4:], uint32(entryPos))
				le2 += 8
				le.PutUint32(out[entryPos:], rva+uint32(blob+blobs.Len()))
				le.PutUint32(out[entryPos+4:], uint32(len(data)))
				entryPos += 16
				blobs.Write(data)
				for blobs.Len()%8 != 0 {
					blobs.WriteByte(0)
				}
			}
		}
	}
	for n, at := range strPos {
		u := utf16.Encode([]rune(n))
		le.PutUint16(out[at:], uint16(len(u)))
		for i, c := range u {
			le.PutUint16(out[at+2+2*i:], c)
		}
	}
	return append(out, blobs.Bytes()...)
}

func countNamed(ks []key) int {
	n := 0
	for _, k := range ks {
		if k.name != "" {
			n++
		}
	}
	return n
}

// IconImage is one image of an .ico file.
type IconImage struct {
	Width, Height int // 1-256
	Colors        uint8
	Planes        uint16
	BitCount      uint16
	Data          []byte // a BMP (BITMAPINFOHEADER onward) or a PNG
}

// ParseICO reads an .ico file.
func ParseICO(b []byte) ([]IconImage, error) {
	if len(b) < 6 || le.Uint16(b) != 0 || le.Uint16(b[2:]) != 1 {
		return nil, errors.New("not an .ico file")
	}
	n := int(le.Uint16(b[4:]))
	if n == 0 || n > 64 || len(b) < 6+16*n {
		return nil, errors.New("not an .ico file: bad image count")
	}
	out := make([]IconImage, n)
	for i := range out {
		e := b[6+16*i:]
		size, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		if size == 0 || uint64(off)+uint64(size) > uint64(len(b)) {
			return nil, fmt.Errorf("not an .ico file: image %d out of range", i+1)
		}
		out[i] = IconImage{Width: dim(e[0]), Height: dim(e[1]), Colors: e[2], Planes: le.Uint16(e[4:]),
			BitCount: le.Uint16(e[6:]), Data: b[off : off+size]}
	}
	return out, nil
}

func dim(b byte) int {
	if b == 0 {
		return 256
	}
	return int(b)
}

// EncodeICO writes an .ico file.
func EncodeICO(images []IconImage) []byte {
	var b bytes.Buffer
	binary.Write(&b, le, [3]uint16{0, 1, uint16(len(images))})
	off := 6 + 16*len(images)
	for _, im := range images {
		b.Write([]byte{byte(im.Width), byte(im.Height), im.Colors, 0})
		binary.Write(&b, le, [2]uint16{im.Planes, im.BitCount})
		binary.Write(&b, le, [2]uint32{uint32(len(im.Data)), uint32(off)})
		off += len(im.Data)
	}
	for _, im := range images {
		b.Write(im.Data)
	}
	return b.Bytes()
}

// IconResources are an icon's images as RT_ICON 1..n and RT_GROUP_ICON group.
func IconResources(images []IconImage, group uint32) []Resource {
	var out []Resource
	var g bytes.Buffer
	binary.Write(&g, le, [3]uint16{0, 1, uint16(len(images))})
	for i, im := range images {
		g.Write([]byte{byte(im.Width), byte(im.Height), im.Colors, 0})
		binary.Write(&g, le, [2]uint16{im.Planes, im.BitCount})
		binary.Write(&g, le, uint32(len(im.Data)))
		binary.Write(&g, le, uint16(i+1))
		out = append(out, Resource{Type: TypeIcon, ID: uint32(i + 1), Lang: LangEnUS, Data: im.Data})
	}
	return append(out, Resource{Type: TypeGroupIcon, ID: group, Lang: LangEnUS, Data: g.Bytes()})
}

// ICOFromResources rebuilds an .ico file from a program's first icon group (the one Explorer shows).
func ICOFromResources(res []Resource) ([]byte, error) {
	icons := map[key][]byte{}
	var groups []Resource
	for _, r := range res {
		if r.TypeName != "" {
			continue
		}
		switch r.Type {
		case TypeIcon:
			icons[key{r.ID, r.Name}] = r.Data
		case TypeGroupIcon:
			groups = append(groups, r)
		}
	}
	if len(groups) == 0 {
		return nil, errors.New("the program has no icon")
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return less(key{groups[i].ID, groups[i].Name}, key{groups[j].ID, groups[j].Name})
	})
	g := groups[0].Data
	if len(g) < 6 || len(g) < 6+14*int(le.Uint16(g[4:])) {
		return nil, errors.New("the program's icon group is damaged")
	}
	var images []IconImage
	for i := 0; i < int(le.Uint16(g[4:])); i++ {
		e := g[6+14*i:]
		data, ok := icons[key{num: uint32(le.Uint16(e[12:]))}]
		if !ok {
			continue
		}
		images = append(images, IconImage{Width: dim(e[0]), Height: dim(e[1]), Colors: e[2],
			Planes: le.Uint16(e[4:]), BitCount: le.Uint16(e[6:]), Data: data})
	}
	if len(images) == 0 {
		return nil, errors.New("the program's icon group has no images")
	}
	return EncodeICO(images), nil
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=fb7bbb2c3800a9bb22b89b4c2ab318aacb0f801dff384b73333e6ecd6206895d ====
