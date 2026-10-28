// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/winres/version.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/winres/version.go
// About: Part of package winres in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

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

// ParseVersion turns "1.2.3" into {1, 2, 3, 0}.
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
func (v VersionInfo) Bytes() []byte {
	ms := func(a, b uint16) uint32 { return uint32(a)<<16 | uint32(b) }
	fixed := new(bytes.Buffer)
	binary.Write(fixed, le, []uint32{
		0xFEEF04BD, 0x00010000, // signature, structure version
		ms(v.Version[0], v.Version[1]), ms(v.Version[2], v.Version[3]), // file version
		ms(v.Version[0], v.Version[1]), ms(v.Version[2], v.Version[3]), // product version
		0x3F, 0, // flags mask, flags
		0x00040004, 1, 0, // VOS_NT_WINDOWS32, VFT_APP, no subtype
		0, 0, // date
	})
	var strs [][]byte
	for _, kv := range v.Strings {
		val := utf16z(kv[1])
		strs = append(strs, node(kv[0], val, uint16(len(val)/2), true))
	}
	table := node("040904B0", nil, 0, true, strs...)
	translation := []byte{0x09, 0x04, 0xB0, 0x04}
	vars := node("VarFileInfo", nil, 0, true, node("Translation", translation, 4, false))
	return node("VS_VERSION_INFO", fixed.Bytes(), uint16(fixed.Len()), false,
		node("StringFileInfo", nil, 0, true, table), vars)
}

// node is one block: wLength, wValueLength, wType, key, padding, value, then each child 32-bit aligned.
// wLength counts everything but trailing padding.
func node(key string, value []byte, valueLen uint16, text bool, children ...[]byte) []byte {
	var b bytes.Buffer
	kind := uint16(0)
	if text {
		kind = 1
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

func pad4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

func utf16z(s string) []byte {
	u := append(utf16.Encode([]rune(s)), 0)
	b := make([]byte, 2*len(u))
	for i, c := range u {
		le.PutUint16(b[2*i:], c)
	}
	return b
}

// ReadVersionInfo decodes what Bytes writes (and what rc.exe writes): the version and the strings.
func ReadVersionInfo(b []byte) ([4]uint16, map[string]string, error) {
	var v [4]uint16
	strs := map[string]string{}
	key, value, children, err := readNode(b)
	if err != nil || key != "VS_VERSION_INFO" || len(value) < 52 || le.Uint32(value) != 0xFEEF04BD {
		return v, nil, errors.New("not a VS_VERSIONINFO block")
	}
	ms, ls := le.Uint32(value[8:]), le.Uint32(value[12:])
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
func readNode(b []byte) (key string, value []byte, children [][]byte, err error) {
	if len(b) < 6 {
		return "", nil, nil, errors.New("version block too short")
	}
	n, valueLen, kind := int(le.Uint16(b)), int(le.Uint16(b[2:])), le.Uint16(b[4:])
	if n > len(b) || n < 6 {
		return "", nil, nil, errors.New("version block length out of range")
	}
	b = b[:n]
	i := 6
	for i+1 < n && (b[i] != 0 || b[i+1] != 0) {
		i += 2
	}
	key = fromUTF16(b[6:i])
	i = align4(i + 2)
	size := valueLen
	if kind == 1 {
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

func align4(i int) int { return (i + 3) &^ 3 }

func fromUTF16(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = le.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=78b0668446eacd1f21a2042ea2beb8356377eaf211a09c76dab9bd0f38ef6ca1 ====
