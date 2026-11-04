// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/winres/winres.go
// About: Package winres reads and writes Windows resources in pure Go.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package winres reads and writes Windows resources in pure Go.
//
// It handles .ico files, icon groups, version info and the resource section of a PE file, with no
// rc.exe or Windows API, so a setup.exe is the same bytes on every machine. The maker
// (internal/maker, build.go) uses it to turn its own exe into a setup.exe: it reads the stub's
// resources (Resources), keeps the manifest, puts in the program's icon (ParseICO, IconResources) and
// version info (VersionInfo), and writes them back (SetResources). ICOFromResources takes the icon
// back out of a packed program.
//
// A Windows program keeps its resources (icons, the manifest, version info ...) in the .rsrc section
// as a three-level tree: resource type, then name or id, then language. Each leaf points at the data
// by its in-memory address (RVA). Decode and Encode read and write that tree (winres.go); pe.go finds
// and replaces the section in an exe; version.go encodes VS_VERSIONINFO. The layouts follow
// Microsoft's "PE Format" specification, section "The .rsrc Section".
//
// Types: Resource (one leaf of the tree), IconImage (one image of an .ico), VersionInfo.
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
	TypeIcon      = 3      // RT_ICON: one image of an icon
	TypeGroupIcon = 14     // RT_GROUP_ICON: an icon's list of images, pointing at RT_ICONs by id
	TypeVersion   = 16     // RT_VERSION: VS_VERSIONINFO
	TypeManifest  = 24     // RT_MANIFEST: the application manifest (Common Controls 6, DPI awareness)
	LangEnUS      = 0x0409 // LANGID English (United States)
)

// Resource is one resource. A named type or name (rare) is kept in TypeName / Name instead of the number.
type Resource struct {
	Type     uint32 // the type id (TypeIcon ...), when TypeName is empty
	TypeName string
	ID       uint32 // the resource id, when Name is empty
	Name     string
	Lang     uint32 // a LANGID; 0 is language-neutral
	Data     []byte
}

// le reads and writes the little-endian numbers every PE and resource structure uses.
var le = binary.LittleEndian

// The resource section's layout, after Microsoft's "PE Format": a tree of directories three levels
// deep (type, name, language), whose last level points at data entries.
const (
	resDirSize       = 16 // IMAGE_RESOURCE_DIRECTORY; its entries follow it
	resDirNamed      = 12 // NumberOfNamedEntries
	resDirIDs        = 14 // NumberOfIdEntries
	resEntrySize     = 8  // IMAGE_RESOURCE_DIRECTORY_ENTRY: a name or id, then what it points at
	resDataEntrySize = 16 // IMAGE_RESOURCE_DATA_ENTRY: the data's address and size, code page, reserved
	resLangLevel     = 2  // the language level, the last one: its entries point at data entries

	resNameIsString    = 0x80000000 // IMAGE_RESOURCE_NAME_IS_STRING: the rest is the offset of a name
	resDataIsDirectory = 0x80000000 // IMAGE_RESOURCE_DATA_IS_DIRECTORY: the rest is the offset of a directory
)

// Decode reads a resource section's tree. rva is the section's address: data entries point by address.
//
// sec starts at the root directory. Every offset read from it is checked, so a damaged section gives
// an error, never a panic; the data is copied out of sec.
//
// How the tree is read: a directory is a 16-byte header whose last two u16s count its named and its
// numbered entries; the 8-byte entries follow it, named ones first. An entry's first u32 is an id,
// or, with the high bit set, the offset of a length-prefixed UTF-16 name. Its second u32 is, with the
// high bit set, the offset of a subdirectory, else the offset of a data entry (the data's RVA and
// size). Offsets are from the section start; data addresses are RVAs, hence rva. walk recurses one
// level per directory, keeping the path of (type, name, language) keys it came by.
func Decode(sec []byte, rva uint32) ([]Resource, error) {
	var out []Resource
	type level struct {
		num  uint32
		name string
	}
	var walk func(off uint32, depth int, path []level) error
	walk = func(off uint32, depth int, path []level) error {
		if depth > resLangLevel || int(off)+resDirSize > len(sec) {
			return errors.New("resource directory out of range")
		}
		n := int(le.Uint16(sec[off+resDirNamed:])) + int(le.Uint16(sec[off+resDirIDs:]))
		for i := 0; i < n; i++ {
			e := int(off) + resDirSize + resEntrySize*i
			if e+resEntrySize > len(sec) {
				return errors.New("resource entry out of range")
			}
			nameField, target := le.Uint32(sec[e:]), le.Uint32(sec[e+4:])
			var lv level
			if nameField&resNameIsString != 0 {
				s, err := dirString(sec, nameField&^resNameIsString)
				if err != nil {
					return err
				}
				lv.name = s
			} else {
				lv.num = nameField
			}
			p := append(append([]level{}, path...), lv)
			if target&resDataIsDirectory != 0 {
				if err := walk(target&^resDataIsDirectory, depth+1, p); err != nil {
					return err
				}
				continue
			}
			if depth != resLangLevel || int(target)+resDataEntrySize > len(sec) {
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

// dirString reads a directory string at off: a u16 count of UTF-16 units, then the units (no NUL).
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

// key is one level's key in the tree: a name when name is set, else the number num.
type key struct {
	num  uint32
	name string
}

// less orders keys as the PE format wants a directory's entries: named ones first, then ids.
// Names compare as Go strings, byte by byte; for ASCII names that is the same order as comparing their
// UTF-16 units. Encode and ICOFromResources both sort with it.
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
//
// Two resources with the same type, name and language keep the last one's data. The directories'
// time stamps and versions stay 0, again so the bytes depend on nothing but res.
//
// Steps:
//  1. Group res into a tree: type, then name or id, then language, each level sorted by less.
//  2. Size every part first: all the directories (each 16 bytes plus 8 per entry), then the 16-byte
//     data entries, then the names (each name once, a u16 length and its UTF-16 units), and round
//     up to 8; the data blobs start there.
//  3. Write the directories depth first (the root, a type's name directory, each of its names'
//     language directories, then the next type): each entry points at its subdirectory (high bit
//     set) or, at the language level, at its data entry; a data entry holds rva plus the blob's offset.
//  4. Write the names, then append the blobs, each padded to 8 bytes.
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
	dirSize := resDirSize + resEntrySize*len(types)
	var names []string
	nData := 0
	for _, tk := range types {
		dirSize += resDirSize + resEntrySize*len(t[tk])
		if tk.name != "" {
			names = append(names, tk.name)
		}
		for _, nk := range sorted(t[tk]) {
			dirSize += resDirSize + resEntrySize*len(t[tk][nk])
			nData += len(t[tk][nk])
			if nk.name != "" {
				names = append(names, nk.name)
			}
		}
	}
	dataEntries := dirSize
	strOff := dataEntries + resDataEntrySize*nData
	strPos := map[string]int{}
	pos := strOff
	for _, n := range names {
		if _, ok := strPos[n]; !ok {
			strPos[n] = pos
			pos += 2 + 2*len(utf16.Encode([]rune(n))) // a u16 length, then the UTF-16 units
		}
	}
	blob := (pos + 7) &^ 7

	// write the tree: typeEntry, nameEntry and langEntry are where the next entry goes in the type
	// directory, the current name directory and the current language directory
	out := make([]byte, blob)
	dirPos, entryPos := 0, dataEntries
	var blobs bytes.Buffer
	writeDir := func(at int, ks []key) int { // returns where the entries start
		le.PutUint16(out[at+resDirNamed:], uint16(countNamed(ks)))
		le.PutUint16(out[at+resDirIDs:], uint16(len(ks)-countNamed(ks)))
		return at + resDirSize
	}
	nameField := func(k key) uint32 {
		if k.name != "" {
			return resNameIsString | uint32(strPos[k.name])
		}
		return k.num
	}
	root := dirPos
	dirPos += resDirSize + resEntrySize*len(types)
	typeEntry := writeDir(root, types)
	for _, tk := range types {
		ids := sorted(t[tk])
		sub := dirPos
		dirPos += resDirSize + resEntrySize*len(ids)
		le.PutUint32(out[typeEntry:], nameField(tk))
		le.PutUint32(out[typeEntry+4:], resDataIsDirectory|uint32(sub))
		typeEntry += resEntrySize
		nameEntry := writeDir(sub, ids)
		for _, nk := range ids {
			langs := make([]key, 0, len(t[tk][nk]))
			for l := range t[tk][nk] {
				langs = append(langs, key{num: l})
			}
			sort.Slice(langs, func(i, j int) bool { return langs[i].num < langs[j].num })
			lsub := dirPos
			dirPos += resDirSize + resEntrySize*len(langs)
			le.PutUint32(out[nameEntry:], nameField(nk))
			le.PutUint32(out[nameEntry+4:], resDataIsDirectory|uint32(lsub))
			nameEntry += resEntrySize
			langEntry := writeDir(lsub, langs)
			for _, l := range langs {
				data := t[tk][nk][l.num]
				le.PutUint32(out[langEntry:], l.num)
				le.PutUint32(out[langEntry+4:], uint32(entryPos))
				langEntry += resEntrySize
				le.PutUint32(out[entryPos:], rva+uint32(blob+blobs.Len()))
				le.PutUint32(out[entryPos+4:], uint32(len(data)))
				entryPos += resDataEntrySize
				blobs.Write(data)
				for blobs.Len()%8 != 0 {
					blobs.WriteByte(0)
				}
			}
		}
	}
	for n, at := range strPos { // map order does not matter: every name's place is already fixed
		u := utf16.Encode([]rune(n))
		le.PutUint16(out[at:], uint16(len(u)))
		for i, c := range u {
			le.PutUint16(out[at+2+2*i:], c)
		}
	}
	return append(out, blobs.Bytes()...)
}

// countNamed is how many of ks are names, for a directory's NumberOfNamedEntries.
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
	Width, Height int    // 1-256
	Colors        uint8  // palette size; 0 for 256 or more colours
	Planes        uint16 // colour planes, 0 or 1
	BitCount      uint16 // bits per pixel (32 for the usual images with alpha)
	Data          []byte // a BMP (BITMAPINFOHEADER onward) or a PNG
}

// ParseICO reads an .ico file.
//
// The format: a 6-byte ICONDIR (reserved 0, type 1 for icons, the image count), then a 16-byte
// ICONDIRENTRY per image (width, height, colour count, reserved, planes, bit count, the data's size
// and file offset). A width or height byte of 0 means 256. Counts above 64 are refused as damage.
// The images' Data slices share b's memory.
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

// dim is an icon entry's width or height byte as pixels: 0 stands for 256, which does not fit a byte.
func dim(b byte) int {
	if b == 0 {
		return 256
	}
	return int(b)
}

// EncodeICO writes an .ico file.
//
// It is ParseICO's inverse: the header, the entries, then the images in order, each entry holding its
// image's offset. byte(256) is 0, which is how the format writes 256.
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
//
// Inside an exe an icon is split up: each image is its own RT_ICON resource, and the RT_GROUP_ICON
// lists them. The group is an .ico header whose entries are 14 bytes (GRPICONDIRENTRY): the file
// offset of an .ico entry is replaced by the u16 id of the image's RT_ICON. Everything is en-US.
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
//
// "First" is in directory order (less): a named group before numbered ones, then the lowest id.
// Images are matched by RT_ICON id whatever their language; an entry whose image is missing is left
// out, and a group with none left is an error, as is a program with no group.
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

// ==== stamp v1 | author=kjFriday | hmac-sha256=e522ca0229363bce1f30efd5a4c93a7ab9b9528deb645d21e53b191c260d1ba0 ====
