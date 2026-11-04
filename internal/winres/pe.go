// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/winres/pe.go
// About: Just enough PE32+ parsing to read and replace a 64-bit program's resource section.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Just enough PE32+ parsing to read and replace a 64-bit program's resource section.
//
// A Windows exe (Portable Executable, "PE") starts with an MS-DOS header whose e_lfanew field points
// at the "PE\0\0" signature. After it come the COFF file header (how many sections), the optional
// header (alignments, image size, checksum, subsystem, and the data directories that locate things
// such as the resources and the signature) and the section table. This file reads only those fields.
//
// Resources reads the resources; SetResources writes new ones. Rewriting is simple only because the
// resource section is the last one, in the file and in memory: it can grow or shrink without moving
// any other section, so no code or address elsewhere needs fixing. Go's linker puts .rsrc (from the
// .syso) last, and pe_windows_test.go checks that it still does.

package winres

import (
	"errors"
	"fmt"
)

// pe holds the offsets of the PE header fields this package reads or patches.
type pe struct {
	opt                  int    // optional header
	sections             int    // first section header
	count                int    // NumberOfSections
	fileAlign, sectAlign uint32 // FileAlignment (section sizes in the file), SectionAlignment (in memory)
}

// The PE32+ layout, after Microsoft's "PE Format": offsets into the MS-DOS header, the COFF file header,
// the optional header and a section header, and the values this package reads or writes there.
const (
	dosHeaderSize = 0x40 // IMAGE_DOS_HEADER, which ends with e_lfanew
	lfanewAt      = 0x3c // e_lfanew: the file offset of the "PE\0\0" signature

	signatureSize  = 4  // "PE\0\0"; the COFF file header follows it
	coffHeaderSize = 20 // IMAGE_FILE_HEADER; the optional header follows it
	coffSections   = 2  // NumberOfSections
	coffOptSize    = 16 // SizeOfOptionalHeader

	magicPE32Plus   = 0x20b // the optional header's Magic in a 64-bit program
	optInitData     = 8     // SizeOfInitializedData
	optSectionAlign = 32    // SectionAlignment
	optFileAlign    = 36    // FileAlignment
	optSizeOfImage  = 56    // SizeOfImage
	optCheckSum     = 64    // CheckSum
	optSubsystem    = 68    // Subsystem
	optDataDirs     = 112   // DataDirectory (in PE32+)
	dataDirCount    = 16    // IMAGE_NUMBEROF_DIRECTORY_ENTRIES
	dataDirSize     = 8     // IMAGE_DATA_DIRECTORY: VirtualAddress, then Size

	sectionHeaderSize  = 40   // IMAGE_SECTION_HEADER
	secVirtualSize     = 8    // VirtualSize
	secVirtualAddress  = 12   // VirtualAddress
	secRawSize         = 16   // SizeOfRawData
	secRawPointer      = 20   // PointerToRawData
	secCharacteristics = 36   // Characteristics
	scnInitializedData = 0x40 // IMAGE_SCN_CNT_INITIALIZED_DATA

	dirResource  = 2 // IMAGE_DIRECTORY_ENTRY_RESOURCE
	dirSecurity  = 4 // IMAGE_DIRECTORY_ENTRY_SECURITY
	subsystemGUI = 2 // IMAGE_SUBSYSTEM_WINDOWS_GUI
)

// parsePE finds the headers of a 64-bit (PE32+) program and checks they lie inside b.
//
// Steps:
//  1. "MZ" and e_lfanew lead to the "PE\0\0" signature; the COFF header follows it.
//  2. The optional header follows the COFF header, and the section table follows the optional
//     header (SizeOfOptionalHeader bytes later).
//  3. The optional header's Magic must be 0x20b (PE32+): the field offsets here are PE32+ ones.
//  4. FileAlignment must be a power of two (alignUp relies on it) and SectionAlignment non-zero.
func parsePE(b []byte) (pe, error) {
	if len(b) < dosHeaderSize || b[0] != 'M' || b[1] != 'Z' {
		return pe{}, errors.New("not a Windows program")
	}
	sig := int(le.Uint32(b[lfanewAt:]))
	if sig+(signatureSize+coffHeaderSize) > len(b) || string(b[sig:sig+signatureSize]) != "PE\x00\x00" {
		return pe{}, errors.New("not a Windows program")
	}
	coff := sig + signatureSize
	h := pe{opt: coff + coffHeaderSize, count: int(le.Uint16(b[coff+coffSections:]))}
	optSize := int(le.Uint16(b[coff+coffOptSize:]))
	h.sections = h.opt + optSize
	if h.opt+optDataDirs+dataDirCount*dataDirSize > len(b) || le.Uint16(b[h.opt:]) != magicPE32Plus {
		return pe{}, errors.New("not a 64-bit Windows program")
	}
	if h.sections+sectionHeaderSize*h.count > len(b) {
		return pe{}, errors.New("section table out of range")
	}
	h.sectAlign, h.fileAlign = le.Uint32(b[h.opt+optSectionAlign:]), le.Uint32(b[h.opt+optFileAlign:])
	if h.fileAlign == 0 || h.fileAlign&(h.fileAlign-1) != 0 || h.sectAlign == 0 {
		return pe{}, errors.New("bad alignment in the PE header")
	}
	return h, nil
}

// dir is data directory i (dirResource, dirSecurity ...): its address and size, both 0 when unused.
// For the security directory the "address" is a file offset, as the PE format defines it.
func (h pe) dir(b []byte, i int) (rva, size uint32) {
	at := h.opt + optDataDirs + dataDirSize*i
	return le.Uint32(b[at:]), le.Uint32(b[at+4:])
}

// rsrc finds the section holding the resource directory and checks it is the last one, in the section
// table, in the file and in memory, so it can grow without moving anything else.
//
// header is the file offset of the section's header. A section holds the directory when its address
// range, the larger of VirtualSize and SizeOfRawData, contains the directory's address.
func (h pe) rsrc(b []byte) (header int, err error) {
	rva, _ := h.dir(b, dirResource)
	if rva == 0 {
		return 0, errors.New("the program has no resources (was it built with its .syso?)")
	}
	last := h.sections + sectionHeaderSize*(h.count-1)
	for i := 0; i < h.count; i++ {
		s := h.sections + sectionHeaderSize*i
		va, vsize, raw := le.Uint32(b[s+secVirtualAddress:]), le.Uint32(b[s+secVirtualSize:]), le.Uint32(b[s+secRawPointer:])
		if rva >= va && rva < va+max(vsize, le.Uint32(b[s+secRawSize:])) {
			header = s
		}
		if s != last && (va >= le.Uint32(b[last+secVirtualAddress:]) || raw >= le.Uint32(b[last+secRawPointer:])) {
			return 0, errors.New("the resource section is not the last section")
		}
	}
	if header != last {
		return 0, errors.New("the resource section is not the last section")
	}
	return header, nil
}

// Resources reads a PE file's resources.
//
// A program with no resource directory gives none and no error. The section is found by its file
// size (SizeOfRawData), and the tree is decoded from the directory's place in it.
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
		va, raw, ptr := le.Uint32(exe[s+secVirtualAddress:]), le.Uint32(exe[s+secRawSize:]), le.Uint32(exe[s+secRawPointer:])
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
//
// exe is not changed; the result is a new slice. The same exe and res always give the same bytes.
//
// Steps:
//  1. Find the resource section (rsrc) and check the directory starts at its very start, so the new
//     section can be written from there.
//  2. Encode res for the section's address and pad it to FileAlignment.
//  3. Copy everything before the section, then the new section; nothing after it is kept.
//  4. Patch the section header's sizes, SizeOfInitializedData (when the section counts as
//     initialised data), SizeOfImage (the section's end rounded to SectionAlignment), and the
//     resource directory.
//  5. Zero the checksum (only drivers and boot files need one) and the security directory: an
//     Authenticode signature, if any, was in the dropped tail and would not match anyway.
//  6. Set the subsystem to GUI, so a setup.exe opens no console window.
func SetResources(exe []byte, res []Resource) ([]byte, error) {
	h, err := parsePE(exe)
	if err != nil {
		return nil, err
	}
	s, err := h.rsrc(exe)
	if err != nil {
		return nil, err
	}
	va, oldRaw, ptr := le.Uint32(exe[s+secVirtualAddress:]), le.Uint32(exe[s+secRawSize:]), le.Uint32(exe[s+secRawPointer:])
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

	le.PutUint32(out[s+secVirtualSize:], uint32(len(data)))
	le.PutUint32(out[s+secRawSize:], raw)
	if le.Uint32(out[s+secCharacteristics:])&scnInitializedData != 0 {
		le.PutUint32(out[h.opt+optInitData:], le.Uint32(out[h.opt+optInitData:])-oldRaw+raw)
	}
	le.PutUint32(out[h.opt+optSizeOfImage:], alignUp(va+uint32(len(data)), h.sectAlign))
	le.PutUint32(out[h.opt+optCheckSum:], 0)
	le.PutUint16(out[h.opt+optSubsystem:], subsystemGUI)
	at := h.opt + optDataDirs + dataDirSize*dirResource
	le.PutUint32(out[at:], va)
	le.PutUint32(out[at+4:], uint32(len(data)))
	at = h.opt + optDataDirs + dataDirSize*dirSecurity
	le.PutUint32(out[at:], 0)
	le.PutUint32(out[at+4:], 0)
	return out, nil
}

// alignUp rounds n up to a multiple of a, which must be a power of two: adding a-1 and clearing the
// low bits (&^ is AND NOT) lands on the next multiple.
func alignUp(n, a uint32) uint32 { return (n + a - 1) &^ (a - 1) }

// ==== stamp v1 | author=kjFriday | hmac-sha256=878872200d8da6de5c8e6f43e12ede95cd817754cc65ec3aacbc70483cd71169 ====
