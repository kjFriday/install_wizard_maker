// ==== ACME Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/payload/payload_test.go
// About: Tests for package payload.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package payload

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// stub is a real PE with nothing after it: this test binary.
func stub(t *testing.T) []byte {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func entries() []Entry {
	return []Entry{Bytes("setup.json", []byte(`{"id":"x"}`)), Bytes("files/a.txt", bytes.Repeat([]byte("abc"), 1000)),
		Bytes("files/sub/b.bin", []byte{0, 1, 2, 3})}
}

func write(t *testing.T, name string, prog []byte, kind Kind, e []Entry) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, prog, kind, e); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRoundTrip(t *testing.T) {
	prog := stub(t)
	path := write(t, "setup.exe", prog, Setup, entries())
	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Kind != Setup || p.Stub != int64(len(prog)) {
		t.Fatalf("kind %d stub %d", p.Kind, p.Stub)
	}
	if b, err := p.ReadAll("setup.json", 100); err != nil || string(b) != `{"id":"x"}` {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := p.ReadAll("setup.json", 5); !errors.Is(err, ErrDamaged) {
		t.Fatalf("limit not kept: %v", err)
	}
	want := bytes.Repeat([]byte("abc"), 1000)
	sum := sha256.Sum256(want)
	var got bytes.Buffer
	if err := p.CopyChecked("files/a.txt", &got, int64(len(want)), hex.EncodeToString(sum[:])); err != nil || !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("copy: %v", err)
	}
	if err := p.CopyChecked("files/a.txt", io.Discard, int64(len(want)), hex.EncodeToString(make([]byte, 32))); !errors.Is(err, ErrDamaged) {
		t.Fatalf("wrong hash accepted: %v", err)
	}
	if err := p.CopyChecked("files/a.txt", io.Discard, 10, hex.EncodeToString(sum[:])); !errors.Is(err, ErrDamaged) {
		t.Fatalf("wrong size accepted: %v", err)
	}
	if !p.Has("files/sub/b.bin") || p.Has("files/nope") {
		t.Fatal("Has")
	}
	if b, err := p.StubBytes(); err != nil || !bytes.Equal(b, prog) {
		t.Fatal("stub bytes differ")
	}
	info, _ := os.Stat(path)
	if info.Size()%8 != 0 {
		t.Fatalf("size %d is not a multiple of 8", info.Size())
	}
}

func TestDeterministic(t *testing.T) {
	prog := stub(t)
	a, _ := os.ReadFile(write(t, "a.exe", prog, Setup, entries()))
	b, _ := os.ReadFile(write(t, "b.exe", prog, Setup, entries()))
	if !bytes.Equal(a, b) {
		t.Fatal("the same input made different bytes")
	}
}

func TestPlainProgramHasNoPayload(t *testing.T) {
	self, _ := os.Executable()
	p, err := Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Kind != None {
		t.Fatalf("kind %d", p.Kind)
	}
}

func TestDamaged(t *testing.T) {
	prog := stub(t)
	good, _ := os.ReadFile(write(t, "setup.exe", prog, Uninstall, entries()))
	cases := map[string][]byte{
		"cut short":     good[:len(good)-10],
		"junk appended": append(append([]byte{}, prog...), bytes.Repeat([]byte{7}, 100)...),
		"few bytes":     append(append([]byte{}, prog...), 1, 2, 3),
		"magic changed": flip(good, len(good)-TrailerSize),
		"offset off":    flip(good, len(good)-16),
		"zip changed":   flip(good, len(prog)+2),
		"bad kind":      flip(good, len(good)-24),
	}
	for name, b := range cases {
		path := filepath.Join(t.TempDir(), "x.exe")
		os.WriteFile(path, b, 0o644)
		p, err := Open(path)
		if name == "zip changed" && err == nil {
			// a changed local header can still parse; reading it must then fail its checks
			_, err = p.ReadAll("setup.json", 100)
			p.Close()
		}
		if !errors.Is(err, ErrDamaged) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func flip(b []byte, i int) []byte {
	c := append([]byte{}, b...)
	c[i] ^= 0x5a
	return c
}

func TestSignatureAfterPayload(t *testing.T) {
	good, _ := os.ReadFile(write(t, "setup.exe", stub(t), Setup, entries()))
	// what signtool does: append a certificate table and point the security directory at it
	cert := make([]byte, 16)
	binary.LittleEndian.PutUint32(cert, 16)
	signed := append(append([]byte{}, good...), cert...)
	opt := int(binary.LittleEndian.Uint32(signed[0x3c:])) + 24
	binary.LittleEndian.PutUint32(signed[opt+144:], uint32(len(good)))
	binary.LittleEndian.PutUint32(signed[opt+148:], 16)
	path := filepath.Join(t.TempDir(), "signed.exe")
	os.WriteFile(path, signed, 0o644)
	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Kind != Setup || !p.Has("files/a.txt") {
		t.Fatal("payload not found before the signature")
	}
}

func TestRejectsEscapingNames(t *testing.T) {
	for _, name := range []string{"../evil.txt", `files\a.txt`, "/abs", "C:/x", "dir/"} {
		var buf bytes.Buffer
		if err := Write(&buf, stub(t), Setup, []Entry{Bytes(name, nil)}); err == nil {
			t.Errorf("%q written", name)
		}
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=ef869ddecc33fbb21fe0052a4d2f6a75da7e0883c7fbc4135aff99b9122ea9b0 ====
