// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/payload/payload.go
// About: Package payload reads and writes the zip and trailer appended to a setup.exe.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package payload reads and writes the zip and trailer appended to a setup.exe.
//
// The payload is the part of a setup.exe after the program itself: a zip of everything to install
// and a 32-byte trailer saying where the zip starts and what the exe is for.
//
//	setup.exe = program (PE) | zip | 0-7 zero bytes | trailer
//	trailer   = "GENCOWZ1" | kind u32 | flags u32 | zip offset u64 | zip length u64   (little-endian)
//
// The padding keeps the file a multiple of 8 bytes, so a code signature appended later needs none and
// the trailer stays right before it.
//
// Why this works: Windows loads a PE (Portable Executable) file by its headers and section table,
// and never reads bytes past the last section, so data appended there (an "overlay") does not
// change how the program runs. The one standard thing that also lives past the sections is the
// Authenticode signature: signtool appends a certificate table and points the PE optional header's
// security data directory (entry 4 of 16) at it. That directory's "address" is a plain file offset,
// and the signature must start on an 8-byte boundary, hence the padding.
//
// Callers: package maker's Build writes setup.exe files (Write). main opens its own exe (Open) and
// runs as the maker, a setup or an uninstaller by its Kind. The wizards read setup.json,
// uninstall.json and app.ico with ReadAll; package install unpacks the program's files with
// CopyChecked; setup writes uninstall.exe from StubBytes and a small Uninstall payload.
//
// Read flow (Open):
//  1. Layout finds where the PE's sections end and where the payload must end (before a signature).
//  2. Nothing in between: a plain program (Kind None).
//  3. Otherwise the last 32 bytes before that end are the trailer; its offset must equal the end of
//     the sections and the padding must be 0-7 bytes, or the file is damaged.
//  4. The zip is opened in place (no copy) and every entry name is checked to stay inside the
//     install folder and to occur once.
//
// Types: Kind, Trailer, Entry (one file to write), File (an opened exe).
package payload

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The trailer's fixed parts: Magic is its first 8 bytes, which mark a payload, and TrailerSize its
// length: 8 magic + 4 kind + 4 flags + 8 offset + 8 length.
const (
	Magic       = "GENCOWZ1"
	TrailerSize = 32
)

// Kind is what an exe is for.
type Kind uint32

// The kinds, as stored in the trailer. None is never written: a trailer whose kind is not Setup
// or Uninstall counts as damaged.
const (
	None      Kind = iota // the maker itself: no payload
	Setup                 // an install wizard
	Uninstall             // the uninstaller a setup leaves in the install folder
)

// ErrDamaged: there is data after the program, but not a whole, unchanged payload.
//
// Every read error that points at a bad download wraps it, so callers test with errors.Is. Its text
// is shown to people, so it says what to do.
var ErrDamaged = errors.New("this setup file is damaged (cut short or changed); download it again")

// stamp is every zip entry's time, so the same input always makes the same bytes.
//
// 2000-01-01 is safely inside the MS-DOS date range a zip header can hold (1980 to 2107).
var stamp = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// Trailer is the last 32 bytes of a setup or uninstall exe.
//
// "Last" means before any Authenticode signature: a signed exe has the certificate table after it.
type Trailer struct {
	Kind   Kind
	Flags  uint32 // reserved; Write leaves it 0 and Open does not look at it
	Offset uint64 // where the zip starts (the program's length)
	Length uint64 // the zip's length
}

// Bytes is the trailer as written: the 32 bytes in the layout of the package doc, little-endian.
func (t Trailer) Bytes() []byte {
	b := make([]byte, TrailerSize)
	copy(b, Magic)
	binary.LittleEndian.PutUint32(b[8:], uint32(t.Kind))
	binary.LittleEndian.PutUint32(b[12:], t.Flags)
	binary.LittleEndian.PutUint64(b[16:], t.Offset)
	binary.LittleEndian.PutUint64(b[24:], t.Length)
	return b
}

// parseTrailer decodes 32 trailer bytes. ok is false unless they start with Magic and name a Setup
// or Uninstall kind; the offset and length are checked by open, which knows the file.
func parseTrailer(b []byte) (Trailer, bool) {
	if len(b) != TrailerSize || string(b[:8]) != Magic {
		return Trailer{}, false
	}
	t := Trailer{Kind: Kind(binary.LittleEndian.Uint32(b[8:])), Flags: binary.LittleEndian.Uint32(b[12:]),
		Offset: binary.LittleEndian.Uint64(b[16:]), Length: binary.LittleEndian.Uint64(b[24:])}
	return t, t.Kind == Setup || t.Kind == Uninstall
}

// Entry is one file to put in the zip.
//
// Open is called only while Write packs this entry, and its reader is closed right after, so a
// payload of many files never holds more than one of them open.
type Entry struct {
	Name string // slash path inside the zip
	Open func() (io.ReadCloser, error)
}

// Bytes is an entry with this content.
func Bytes(name string, b []byte) Entry {
	return Entry{Name: name, Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }}
}

// FromFile is an entry read from a file when it is written.
func FromFile(name, path string) Entry {
	return Entry{Name: name, Open: func() (io.ReadCloser, error) { return os.Open(path) }}
}

// counter passes writes on to w and counts the bytes written, so Write knows where the zip ends
// without needing a seekable writer.
type counter struct {
	w io.Writer
	n int64
}

// Write writes p to the underlying writer and adds what was written to n.
func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// Write writes the program, a zip of the entries (in this order) and the trailer. Entries are
// compressed at the best level and dated 2000-01-01, so equal input gives equal bytes.
//
// stub is the program part, written unchanged. An entry whose name could leave the install folder
// is refused before anything of it is written, and a failing entry stops the write; w then holds a
// partial file, which the caller discards.
//
// Steps:
//  1. Write the stub through a counter.
//  2. Write the zip: Deflate at flate.BestCompression, fixed dates, entries in the given order.
//  3. Work out the zip's length from the count, then pad with zeros so the whole file, trailer
//     included, is a multiple of 8 bytes.
//  4. Write the trailer with the zip's offset (the stub's length) and length.
func Write(w io.Writer, stub []byte, kind Kind, entries []Entry) error {
	c := &counter{w: w}
	if _, err := c.Write(stub); err != nil {
		return err
	}
	z := zip.NewWriter(c)
	z.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestCompression)
	})
	for _, e := range entries {
		if !localName(e.Name) {
			return fmt.Errorf("bad entry name %q", e.Name)
		}
		dst, err := z.CreateHeader(&zip.FileHeader{Name: e.Name, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return err
		}
		src, err := e.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
	}
	if err := z.Close(); err != nil {
		return err
	}
	length := c.n - int64(len(stub))
	// bytes needed to round c.n+32 up to a multiple of 8; the outer % 8 makes "already aligned" 0, not 8
	pad := (8 - (c.n+TrailerSize)%8) % 8
	if _, err := c.Write(make([]byte, pad)); err != nil {
		return err
	}
	_, err := c.Write(Trailer{Kind: kind, Offset: uint64(len(stub)), Length: uint64(length)}.Bytes())
	return err
}

// localName: a zip entry name that can only ever mean a path inside the folder it is unpacked to.
//
// This guards against "zip slip", where an archive names "../x" or an absolute path to write
// outside the target folder. A backslash is refused because Windows reads it as a separator that
// the zip format does not; a trailing "/" is a folder entry, which the payload never has.
// filepath.IsLocal then refuses "..", absolute and drive paths ("C:/x") and reserved names.
func localName(name string) bool {
	return name != "" && !strings.Contains(name, `\`) && !strings.HasSuffix(name, "/") &&
		filepath.IsLocal(filepath.FromSlash(name))
}

// File is an opened exe: what it is for and, for a setup or an uninstaller, the zip inside.
//
// It keeps the exe open until Close; entries are read from it on demand. index maps each entry
// name to its zip header and is nil for a plain program (Kind None).
type File struct {
	Kind  Kind
	Stub  int64 // length of the program part, where the zip starts
	f     *os.File
	index map[string]*zip.File
}

// Open reads an exe's trailer. The maker itself has none (Kind None); bytes after the program that
// are not a whole payload give ErrDamaged. An Authenticode signature after the payload is allowed.
//
// A file that is not a PE at all gives a "not a Windows program" error. On any error the file is
// closed again; on success the caller closes it with Close.
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	p, err := open(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return p, nil
}

// open does Open's work on an opened file; see the read flow in the package doc.
//
// Steps:
//  1. Find the end of the sections (end) and where the payload must end (tail).
//  2. tail == end: nothing appended, a plain program.
//  3. Read the 32 bytes before tail as the trailer and check it: valid magic and kind, the zip
//     starting exactly at end, and 0-7 padding bytes between the zip and the trailer.
//  4. Open the zip on a section of the file and index its entries, refusing unsafe or repeated names.
func open(f *os.File) (*File, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	end, tail, err := Layout(f, info.Size())
	if err != nil {
		return nil, err
	}
	if tail == end {
		return &File{Kind: None, Stub: end, f: f}, nil
	}
	if tail-end < TrailerSize {
		return nil, ErrDamaged
	}
	b := make([]byte, TrailerSize)
	if _, err := f.ReadAt(b, tail-TrailerSize); err != nil {
		return nil, err
	}
	t, ok := parseTrailer(b)
	pad := tail - TrailerSize - int64(t.Offset) - int64(t.Length)
	if !ok || int64(t.Offset) != end || pad < 0 || pad >= 8 {
		return nil, ErrDamaged
	}
	z, err := zip.NewReader(io.NewSectionReader(f, int64(t.Offset), int64(t.Length)), int64(t.Length))
	if err != nil {
		return nil, ErrDamaged
	}
	// a repeated name would let two entries claim one path; the index keeps one, so repeats are refused
	index := map[string]*zip.File{}
	for _, zf := range z.File {
		if !localName(zf.Name) || index[zf.Name] != nil {
			return nil, ErrDamaged
		}
		index[zf.Name] = zf
	}
	return &File{Kind: t.Kind, Stub: end, f: f, index: index}, nil
}

// Layout finds where a PE file's sections end, and where its payload must end: the file's end, or the
// start of an Authenticode signature that comes last.
//
// size is the file's length in bytes. end is the larger of the headers' size and the end of the
// last section with raw data; tail is size, or the signature's file offset when the security data
// directory points at a table that starts at or after end and runs exactly to the end of the file.
// A signature anywhere else is ignored, so its bytes count as payload and the trailer check fails.
//
// Steps:
//  1. Parse the PE headers with debug/pe; anything else is "not a Windows program".
//  2. Take the 16 data directories and SizeOfHeaders from the 32- or 64-bit optional header.
//  3. Extend end over every section's raw data (PointerToRawData + SizeOfRawData).
//  4. Pull tail back to the certificate table when it is the last thing in the file.
func Layout(r io.ReaderAt, size int64) (end, tail int64, err error) {
	pf, err := pe.NewFile(r)
	if err != nil {
		return 0, 0, fmt.Errorf("not a Windows program: %w", err)
	}
	var dirs [16]pe.DataDirectory
	switch oh := pf.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		dirs, end = oh.DataDirectory, int64(oh.SizeOfHeaders)
	case *pe.OptionalHeader32:
		dirs, end = oh.DataDirectory, int64(oh.SizeOfHeaders)
	}
	for _, s := range pf.Sections {
		// Offset and Size are the section's place in the file; a section with no raw data (.bss) has
		// Size 0 and may carry any Offset, so it is skipped
		if e := int64(s.Offset) + int64(s.Size); s.Size > 0 && e > end {
			end = e
		}
	}
	tail = size
	if sig := dirs[pe.IMAGE_DIRECTORY_ENTRY_SECURITY]; sig.VirtualAddress != 0 && int64(sig.VirtualAddress) >= end &&
		int64(sig.VirtualAddress)+int64(sig.Size) == size {
		tail = int64(sig.VirtualAddress) // for this directory the "address" is a file offset
	}
	if end > tail {
		return 0, 0, errors.New("not a Windows program: sections run past the end of the file")
	}
	return end, tail, nil
}

// Close closes the exe; the File must not be used afterwards.
func (p *File) Close() error { return p.f.Close() }

// Has reports whether the zip holds name (a slash path, matched exactly).
func (p *File) Has(name string) bool { return p.index[name] != nil }

// ReadAll reads a whole entry of at most limit bytes.
//
// A missing entry, a read or decompression error, or an entry over limit all wrap ErrDamaged. The
// limit stops a damaged or hostile zip from inflating into a huge allocation.
func (p *File) ReadAll(name string, limit int64) ([]byte, error) {
	zf := p.index[name]
	if zf == nil {
		return nil, fmt.Errorf("%s is missing: %w", name, ErrDamaged)
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %v: %w", name, err, ErrDamaged)
	}
	defer rc.Close()
	// read one byte past the limit: getting it back proves the entry is too large
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %v: %w", name, err, ErrDamaged)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes: %w", name, limit, ErrDamaged)
	}
	return b, nil
}

// CopyChecked copies an entry to w; it must be exactly size bytes with this SHA-256 (hex).
//
// size and sum come from setup.json's file list, which the maker wrote at build time. The bytes go
// to w as they are read, so on a mismatch w has already received them: the caller writes to a
// staging file and drops it on error. Every failure wraps ErrDamaged.
func (p *File) CopyChecked(name string, w io.Writer, size int64, sum string) error {
	zf := p.index[name]
	if zf == nil {
		return fmt.Errorf("%s is missing: %w", name, ErrDamaged)
	}
	rc, err := zf.Open()
	if err != nil {
		return fmt.Errorf("%s: %v: %w", name, err, ErrDamaged)
	}
	defer rc.Close()
	h := sha256.New()
	// size+1 again: a longer entry is caught by n != size without reading all of it
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(rc, size+1))
	if err != nil {
		return fmt.Errorf("%s: %v: %w", name, err, ErrDamaged)
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != sum {
		return fmt.Errorf("%s does not match its SHA-256: %w", name, ErrDamaged)
	}
	return nil
}

// StubBytes is the program part: what an uninstaller is made from.
//
// It reads the first Stub bytes of the exe into memory; a signature, if any, is not included.
func (p *File) StubBytes() ([]byte, error) {
	b := make([]byte, p.Stub)
	_, err := p.f.ReadAt(b, 0)
	return b, err
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=f65fc30d8f78527813c06a89f387f96d9a549b891219e018048a320226225017 ====
