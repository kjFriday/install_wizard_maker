// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/winres/pe.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/winres/pe.go
// About: Part of package winres in install_wizard_maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package winres

import (
	"errors"
	"fmt"
)

// pe holds the offsets of the PE header fields this package reads or patches.
type pe struct {
	opt                  int // optional header
	sections             int // first section header
	count                int
	fileAlign, sectAlign uint32
}

const (
	sectionHeaderSize = 40
	dirResource       = 2
	dirSecurity       = 4
	subsystemGUI      = 2
)

func parsePE(b []byte) (pe, error) {
	if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
		return pe{}, errors.New("not a Windows program")
	}
	sig := int(le.Uint32(b[0x3c:]))
	if sig+24 > len(b) || string(b[sig:sig+4]) != "PE\x00\x00" {
		return pe{}, errors.New("not a Windows program")
	}
	coff := sig + 4
	h := pe{opt: coff + 20, count: int(le.Uint16(b[coff+2:]))}
	optSize := int(le.Uint16(b[coff+16:]))
	h.sections = h.opt + optSize
	if h.opt+112+16*8 > len(b) || le.Uint16(b[h.opt:]) != 0x20b {
		return pe{}, errors.New("not a 64-bit Windows program")
	}
	if h.sections+sectionHeaderSize*h.count > len(b) {
		return pe{}, errors.New("section table out of range")
	}
	h.sectAlign, h.fileAlign = le.Uint32(b[h.opt+32:]), le.Uint32(b[h.opt+36:])
	if h.fileAlign == 0 || h.fileAlign&(h.fileAlign-1) != 0 || h.sectAlign == 0 {
		return pe{}, errors.New("bad alignment in the PE header")
	}
	return h, nil
}

func (h pe) dir(b []byte, i int) (rva, size uint32) {
	at := h.opt + 112 + 8*i
	return le.Uint32(b[at:]), le.Uint32(b[at+4:])
}

// rsrc finds the section holding the resource directory and checks it is the last one, in the section
// table, in the file and in memory, so it can grow without moving anything else.
func (h pe) rsrc(b []byte) (header int, err error) {
	rva, _ := h.dir(b, dirResource)
	if rva == 0 {
		return 0, errors.New("the program has no resources (was it built with its .syso?)")
	}
	last := h.sections + sectionHeaderSize*(h.count-1)
	for i := 0; i < h.count; i++ {
		s := h.sections + sectionHeaderSize*i
		va, vsize, raw := le.Uint32(b[s+12:]), le.Uint32(b[s+8:]), le.Uint32(b[s+20:])
		if rva >= va && rva < va+max(vsize, le.Uint32(b[s+16:])) {
			header = s
		}
		if s != last && (va >= le.Uint32(b[last+12:]) || raw >= le.Uint32(b[last+20:])) {
			return 0, errors.New("the resource section is not the last section")
		}
	}
	if header != last {
		return 0, errors.New("the resource section is not the last section")
	}
	return header, nil
}

// Resources reads a PE file's resources.
func Resources(exe []byte) ([]Resource, error) {
	h, err := parsePE(exe)
	if err != nil {
		return nil, err
	}
	rva, _ := h.dir(exe, dirResource)
	if rva == 0 {
		return nil, nil
	}
	for i := 0; i < h.count; i++ {
		s := h.sections + sectionHeaderSize*i
		va, raw, ptr := le.Uint32(exe[s+12:]), le.Uint32(exe[s+16:]), le.Uint32(exe[s+20:])
		if rva >= va && rva < va+raw {
			if uint64(ptr)+uint64(raw) > uint64(len(exe)) {
				return nil, errors.New("resource section out of range")
			}
			sec := exe[ptr+(rva-va) : ptr+raw]
			return Decode(sec, rva)
		}
	}
	return nil, errors.New("resource directory is in no section")
}

// SetResources returns exe with its resources replaced by res. The resource section must be the last
// one (Go's linker always puts it there). Whatever followed the sections (a payload, a signature) is
// dropped, the security directory and checksum are cleared, and the subsystem is set to GUI.
func SetResources(exe []byte, res []Resource) ([]byte, error) {
	h, err := parsePE(exe)
	if err != nil {
		return nil, err
	}
	s, err := h.rsrc(exe)
	if err != nil {
		return nil, err
	}
	va, oldRaw, ptr := le.Uint32(exe[s+12:]), le.Uint32(exe[s+16:]), le.Uint32(exe[s+20:])
	if rva, _ := h.dir(exe, dirResource); rva != va {
		return nil, fmt.Errorf("resource directory starts inside its section (at %#x, section at %#x)", rva, va)
	}
	if int(ptr) > len(exe) {
		return nil, errors.New("resource section out of range")
	}
	data := Encode(res, va)
	raw := alignUp(uint32(len(data)), h.fileAlign)
	out := make([]byte, int(ptr)+int(raw))
	copy(out, exe[:ptr])
	copy(out[ptr:], data)

	le.PutUint32(out[s+8:], uint32(len(data))) // VirtualSize
	le.PutUint32(out[s+16:], raw)              // SizeOfRawData
	if le.Uint32(out[s+36:])&0x40 != 0 {       // IMAGE_SCN_CNT_INITIALIZED_DATA
		le.PutUint32(out[h.opt+8:], le.Uint32(out[h.opt+8:])-oldRaw+raw)
	}
	le.PutUint32(out[h.opt+56:], alignUp(va+uint32(len(data)), h.sectAlign)) // SizeOfImage
	le.PutUint32(out[h.opt+64:], 0)                                          // CheckSum
	le.PutUint16(out[h.opt+68:], subsystemGUI)
	at := h.opt + 112 + 8*dirResource
	le.PutUint32(out[at:], va)
	le.PutUint32(out[at+4:], uint32(len(data)))
	at = h.opt + 112 + 8*dirSecurity
	le.PutUint32(out[at:], 0)
	le.PutUint32(out[at+4:], 0)
	return out, nil
}

func alignUp(n, a uint32) uint32 { return (n + a - 1) &^ (a - 1) }

// ==== stamp v1 | author=kjFriday | hmac-sha256=09093b09703c70c41436494079f423937ec0523798714d6d981a8754180c4a3c ====
