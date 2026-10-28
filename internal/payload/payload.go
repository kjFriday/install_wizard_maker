// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/payload/payload.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/payload/payload.go
// About: Package payload is the part of a setup.exe after the program itself: a zip of everything...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package payload is the part of a setup.exe after the program itself: a zip of everything to install
// and a 32-byte trailer saying where the zip starts and what the exe is for.
//
//	setup.exe = program (PE) | zip | 0-7 zero bytes | trailer
//	trailer   = "GENCOWZ1" | kind u32 | flags u32 | zip offset u64 | zip length u64   (little-endian)
//
// The padding keeps the file a multiple of 8 bytes, so a code signature appended later needs none and
// the trailer stays right before it.
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

const (
	Magic       = "GENCOWZ1"
	TrailerSize = 32
)

// Kind is what an exe is for.
type Kind uint32

const (
	None      Kind = iota // the maker itself: no payload
	Setup                 // an install wizard
	Uninstall             // the uninstaller a setup leaves in the install folder
)

// ErrDamaged: there is data after the program, but not a whole, unchanged payload.
var ErrDamaged = errors.New("this setup file is damaged (cut short or changed); download it again")

// stamp is every zip entry's time, so the same input always makes the same bytes.
var stamp = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// Trailer is the last 32 bytes of a setup or uninstall exe.
type Trailer struct {
	Kind   Kind
	Flags  uint32
	Offset uint64 // where the zip starts (the program's length)
	Length uint64 // the zip's length
}

// Bytes is the trailer as written.
func (t Trailer) Bytes() []byte {
	b := make([]byte, TrailerSize)
	copy(b, Magic)
	binary.LittleEndian.PutUint32(b[8:], uint32(t.Kind))
	binary.LittleEndian.PutUint32(b[12:], t.Flags)
	binary.LittleEndian.PutUint64(b[16:], t.Offset)
	binary.LittleEndian.PutUint64(b[24:], t.Length)
	return b
}

func parseTrailer(b []byte) (Trailer, bool) {
	if len(b) != TrailerSize || string(b[:8]) != Magic {
		return Trailer{}, false
	}
	t := Trailer{Kind: Kind(binary.LittleEndian.Uint32(b[8:])), Flags: binary.LittleEndian.Uint32(b[12:]),
		Offset: binary.LittleEndian.Uint64(b[16:]), Length: binary.LittleEndian.Uint64(b[24:])}
	return t, t.Kind == Setup || t.Kind == Uninstall
}

// Entry is one file to put in the zip.
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

type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// Write writes the program, a zip of the entries (in this order) and the trailer. Entries are
// compressed at the best level and dated 2000-01-01, so equal input gives equal bytes.
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
	pad := (8 - (c.n+TrailerSize)%8) % 8
	if _, err := c.Write(make([]byte, pad)); err != nil {
		return err
	}
	_, err := c.Write(Trailer{Kind: kind, Offset: uint64(len(stub)), Length: uint64(length)}.Bytes())
	return err
}

// localName: a zip entry name that can only ever mean a path inside the folder it is unpacked to.
func localName(name string) bool {
	return name != "" && !strings.Contains(name, `\`) && !strings.HasSuffix(name, "/") &&
		filepath.IsLocal(filepath.FromSlash(name))
}

// File is an opened exe: what it is for and, for a setup or an uninstaller, the zip inside.
type File struct {
	Kind  Kind
	Stub  int64 // length of the program part, where the zip starts
	f     *os.File
	index map[string]*zip.File
}

// Open reads an exe's trailer. The maker itself has none (Kind None); bytes after the program that
// are not a whole payload give ErrDamaged. An Authenticode signature after the payload is allowed.
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
		if e := int64(s.Offset) + int64(s.Size); s.Size > 0 && e > end {
			end = e
		}
	}
	tail = size
	if sig := dirs[4]; sig.VirtualAddress != 0 && int64(sig.VirtualAddress) >= end &&
		int64(sig.VirtualAddress)+int64(sig.Size) == size {
		tail = int64(sig.VirtualAddress) // for this directory the "address" is a file offset
	}
	if end > tail {
		return 0, 0, errors.New("not a Windows program: sections run past the end of the file")
	}
	return end, tail, nil
}

// Close closes the exe.
func (p *File) Close() error { return p.f.Close() }

// Has reports whether the zip holds name.
func (p *File) Has(name string) bool { return p.index[name] != nil }

// ReadAll reads a whole entry of at most limit bytes.
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
func (p *File) StubBytes() ([]byte, error) {
	b := make([]byte, p.Stub)
	_, err := p.f.ReadAt(b, 0)
	return b, err
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=477e5e9b6a8fb941696f8ce2e0e1d2ef0a03ac8929e7b2cc66b3255a6732bdac ====
