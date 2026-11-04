// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/winres/version.go
// About: Version info: the VS_VERSIONINFO resource that Explorer's Properties > Details shows.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Version info: the VS_VERSIONINFO resource that Explorer's Properties > Details shows.
//
// VS_VERSIONINFO is a tree of blocks. Every block has the same shape: a 6-byte header (wLength, the
// block's size; wValueLength; wType, 1 for text and 0 for bytes), a NUL-terminated UTF-16 key,
// padding to 4 bytes, the value, padding, then the child blocks, each starting 4-byte aligned. The
// tree this package writes:
//
//	VS_VERSION_INFO            value: VS_FIXEDFILEINFO (the version numbers)
//	  StringFileInfo
//	    040904B0               one table: language 0409 (en-US), code page 04B0 (1200, UTF-16)
//	      CompanyName ...      one block per string, the value as text
//	  VarFileInfo
//	    Translation            value: the language and code page of the table above
//
// VersionInfo.Bytes writes it; ReadVersionInfo reads it back (and what rc.exe writes). The maker
// fills it from setup.json (internal/maker, build.go).

package winres

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"unicode/utf16"
)

// VersionInfo is a VS_VERSIONINFO resource: the numbers and text Explorer shows under Properties > Details.
type VersionInfo struct {
	Version [4]uint16   // file and product version
	Strings [][2]string // key, value; in this order (CompanyName, FileDescription, ...)
}

// VS_VERSIONINFO values: the VS_FIXEDFILEINFO fields this package writes or reads, and the header every
// block starts with.
const (
	fixedSignature     = 0xFEEF04BD // dwSignature
	fixedStrucVersion  = 0x00010000 // dwStrucVersion: 1.0
	fixedFileVersionMS = 8          // dwFileVersionMS
	fixedFileVersionLS = 12         // dwFileVersionLS
	fixedFileInfoSize  = 52         // sizeof(VS_FIXEDFILEINFO)
	fileFlagsMask      = 0x3F       // VS_FFI_FILEFLAGSMASK
	osNTWindows32      = 0x00040004 // VOS_NT_WINDOWS32
	fileTypeApp        = 1          // VFT_APP

	nodeHeaderSize = 6 // wLength, wValueLength, wType
	valueBinary    = 0 // wType of a block whose value is bytes
	valueText      = 1 // wType of a block whose value is text
)

// ParseVersion turns "1.2.3" into {1, 2, 3, 0}.
//
// It does not validate: a part that is not a number counts as 0, a fifth part stays in the fourth
// (and so reads as 0), and numbers above 65535 wrap. Callers pass versions that spec has already
// checked.
func ParseVersion(s string) [4]uint16 {
	var v [4]uint16
	for i, part := range strings.SplitN(s, ".", 4) {
		n, _ := strconv.Atoi(part)
		v[i] = uint16(n)
	}
	return v
}

// Resource is the version info as RT_VERSION 1.
func (v VersionInfo) Resource() Resource {
	return Resource{Type: TypeVersion, ID: 1, Lang: LangEnUS, Data: v.Bytes()}
}

// Bytes encodes the version info (language 0409, Unicode code page 04B0).
//
// The file and product versions are both v.Version, each split into two u32s (major<<16 | minor,
// build<<16 | revision). The date is 0 and nothing else varies, so the bytes depend only on v.
func (v VersionInfo) Bytes() []byte {
	ms := func(a, b uint16) uint32 { return uint32(a)<<16 | uint32(b) }
	fixed := new(bytes.Buffer)
	binary.Write(fixed, le, []uint32{
		fixedSignature, fixedStrucVersion,
		ms(v.Version[0], v.Version[1]), ms(v.Version[2], v.Version[3]), // file version
		ms(v.Version[0], v.Version[1]), ms(v.Version[2], v.Version[3]), // product version
		fileFlagsMask, 0, // flags
		osNTWindows32, fileTypeApp, 0, // no subtype
		0, 0, // date
	})
	var strs [][]byte
	for _, kv := range v.Strings {
		val := utf16z(kv[1])
		strs = append(strs, node(kv[0], val, uint16(len(val)/2), true))
	}
	table := node("040904B0", nil, 0, true, strs...)
	translation := []byte{0x09, 0x04, 0xB0, 0x04} // LangEnUS, then code page 1200 (Unicode): two u16, little-endian
	vars := node("VarFileInfo", nil, 0, true, node("Translation", translation, 4, false))
	return node("VS_VERSION_INFO", fixed.Bytes(), uint16(fixed.Len()), false,
		node("StringFileInfo", nil, 0, true, table), vars)
}

// node is one block: wLength, wValueLength, wType, key, padding, value, then each child 32-bit aligned.
// wLength counts everything but trailing padding.
//
// valueLen is wValueLength as the format counts it: UTF-16 units (the NUL included) for a text value,
// bytes for a binary one. wLength is filled in last, once the size is known.
func node(key string, value []byte, valueLen uint16, text bool, children ...[]byte) []byte {
	var b bytes.Buffer
	kind := uint16(valueBinary)
	if text {
		kind = valueText
	}
	binary.Write(&b, le, [3]uint16{0, valueLen, kind})
	b.Write(utf16z(key))
	pad4(&b)
	b.Write(value)
	for _, c := range children {
		pad4(&b)
		b.Write(c)
	}
	out := b.Bytes()
	le.PutUint16(out, uint16(len(out)))
	return out
}

// pad4 writes zero bytes until b's length is a multiple of 4.
func pad4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

// utf16z is s as little-endian UTF-16 with a NUL at the end.
func utf16z(s string) []byte {
	u := append(utf16.Encode([]rune(s)), 0)
	b := make([]byte, 2*len(u))
	for i, c := range u {
		le.PutUint16(b[2*i:], c)
	}
	return b
}

// ReadVersionInfo decodes what Bytes writes (and what rc.exe writes): the version and the strings.
//
// The version is the file version from VS_FIXEDFILEINFO. The strings of every StringFileInfo table
// are merged into one map, whatever their language; trailing NULs are cut. VarFileInfo is skipped.
func ReadVersionInfo(b []byte) ([4]uint16, map[string]string, error) {
	var v [4]uint16
	strs := map[string]string{}
	key, value, children, err := readNode(b)
	if err != nil || key != "VS_VERSION_INFO" || len(value) < fixedFileInfoSize || le.Uint32(value) != fixedSignature {
		return v, nil, errors.New("not a VS_VERSIONINFO block")
	}
	ms, ls := le.Uint32(value[fixedFileVersionMS:]), le.Uint32(value[fixedFileVersionLS:])
	v = [4]uint16{uint16(ms >> 16), uint16(ms), uint16(ls >> 16), uint16(ls)}
	for _, c := range children {
		k, _, tables, err := readNode(c)
		if err != nil {
			return v, nil, err
		}
		if k != "StringFileInfo" {
			continue
		}
		for _, t := range tables {
			_, _, entries, err := readNode(t)
			if err != nil {
				return v, nil, err
			}
			for _, e := range entries {
				name, val, _, err := readNode(e)
				if err != nil {
					return v, nil, err
				}
				strs[name] = strings.TrimRight(fromUTF16(val), "\x00")
			}
		}
	}
	return v, strs, nil
}

// readNode splits one block into its key, value and children.
//
// It is lenient where real files differ: a value said to run past the block is cut at the block's
// end, and a stray byte at the very end is ignored. A child of length 0 or past the block is an error.
//
// Steps:
//  1. Read the header and limit b to wLength.
//  2. The key runs to the first NUL u16; the value starts at the next 4-byte boundary.
//  3. The value's size is wValueLength, doubled for text (which counts UTF-16 units).
//  4. Children follow from the next 4-byte boundary, each wLength long.
func readNode(b []byte) (key string, value []byte, children [][]byte, err error) {
	if len(b) < nodeHeaderSize {
		return "", nil, nil, errors.New("version block too short")
	}
	n, valueLen, kind := int(le.Uint16(b)), int(le.Uint16(b[2:])), le.Uint16(b[4:])
	if n > len(b) || n < nodeHeaderSize {
		return "", nil, nil, errors.New("version block length out of range")
	}
	b = b[:n]
	i := nodeHeaderSize
	for i+1 < n && (b[i] != 0 || b[i+1] != 0) {
		i += 2
	}
	key = fromUTF16(b[nodeHeaderSize:i])
	i = align4(i + 2)
	size := valueLen
	if kind == valueText {
		size *= 2 // text values count characters
	}
	if i+size > n {
		size = n - i
		if size < 0 {
			return "", nil, nil, errors.New("version value out of range")
		}
	}
	value = b[i : i+size]
	i = align4(i + size)
	for i < n {
		if i+2 > n {
			break
		}
		cn := int(le.Uint16(b[i:]))
		if cn == 0 || i+cn > n {
			return "", nil, nil, errors.New("version child out of range")
		}
		children = append(children, b[i:i+cn])
		i = align4(i + cn)
	}
	return key, value, children, nil
}

// align4 rounds i up to a multiple of 4.
func align4(i int) int { return (i + 3) &^ 3 }

// fromUTF16 decodes little-endian UTF-16 bytes; an odd last byte is ignored.
func fromUTF16(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = le.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=9a311b5b76a8d243b15fe849ffe15a0a1d5d47962266f826aa40d24612635908 ====
