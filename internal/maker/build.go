// ==== Generic Company Inc. portfolio ====================================
// Project: wizard_maker | File: internal/maker/build.go
// About: Package maker is the wizard maker: Build makes a setup.exe, Main is its command line.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's tools/stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// Package maker is the wizard maker: Build makes a setup.exe, Main is its command line.
//
// main.go calls Main when the running exe carries no payload, that is, when it is the maker rather
// than a setup or an uninstaller. Main then builds from the command line, installs or uninstalls the
// maker itself, or opens the form window.
//
// Files:
//   - build.go: Build, which turns a setup.json (spec.Spec) into a setup.exe.
//   - cli.go: Main and the build command.
//   - form.go: Form, the window's fields as plain data, and the path helpers the window uses.
//   - self.go: the maker's own install and uninstall.
//   - window_*_windows.go: the Win32 form window.
//
// A setup.exe is the maker's own exe with the program's icon and version info put in its resources,
// followed by a zip payload (package payload). The same exe, run, finds the payload and becomes the
// setup wizard. Start reading at Build.
package maker

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"generic-company/wizard/internal/app"
	"generic-company/wizard/internal/payload"
	"generic-company/wizard/internal/spec"
	"generic-company/wizard/internal/winres"
)

// Result describes a setup.exe that was built.
type Result struct {
	Path   string // where the setup.exe was written (absolute)
	Files  int    // packed files
	Bytes  int64  // their total size
	Size   int64  // the setup.exe's size
	SHA256 string // the setup.exe's hex SHA-256; the same input gives the same value
}

// SpecError means setup.json itself is wrong (as opposed to a file that could not be read or written).
// The command line exits with app.ExitUsage for it, and the window lists its lines as problems to fix.
type SpecError struct{ Err error }

// Error is the wrapped error's text; spec.Check joins its problems one per line.
func (e *SpecError) Error() string { return e.Err.Error() }

// Unwrap lets errors.Is and errors.As see the wrapped error.
func (e *SpecError) Unwrap() error { return e.Err }

// Build makes the setup.exe for s. stub is the maker's own exe: a setup.exe is a copy of it with the
// program's icon and version info, then the payload. logf, if set, hears each step.
//
// The output is reproducible: the same spec and files give a byte-identical setup.exe, wherever the
// folder is (payload.Write fixes zip times; spec.Sources sorts; the manifest holds no build paths).
// Errors in the spec, its files list, license or icon come back as *SpecError; read and write errors
// come back plain.
//
// Steps:
//  1. Check the spec, list the files to pack (spec.Sources) and read the license text.
//  2. Pick the icon (iconFor) and parse it.
//  3. Make the setup's program part: the stub with that icon and version info (setupProgram).
//  4. Write program + payload to a temp file beside the output, hashing it as it goes. Payload entries
//     in order: files/<path> for each file (hashed while read), app.ico, then setup.json last.
//  5. Rename the temp file onto the output path, so a failed build never leaves half a setup.exe.
//
// Build holds no shared state; the window runs it on a worker goroutine, so its logf there must be
// safe to call from that goroutine.
func Build(s *spec.Spec, stub []byte, logf func(format string, a ...any)) (Result, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if err := s.Check(); err != nil {
		return Result{}, &SpecError{err}
	}
	src, err := s.Sources()
	if err != nil {
		return Result{}, &SpecError{err}
	}
	license, err := s.LicenseText()
	if err != nil {
		return Result{}, &SpecError{err}
	}
	ico, from, err := iconFor(s, stub)
	if err != nil {
		return Result{}, &SpecError{err}
	}
	images, err := winres.ParseICO(ico)
	if err != nil {
		return Result{}, &SpecError{fmt.Errorf("icon: %w", err)}
	}
	logf("Icon: %s (%d sizes)", from, len(images))
	prog, err := setupProgram(s, stub, images)
	if err != nil {
		return Result{}, err
	}
	out, _ := s.OutputPath() // s.Check has already vetted the output path
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return Result{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".setup-*.tmp")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed

	// files[i] is filled in by hashed's reader when payload.Write closes entry i; the setup.json
	// entry, last, reads them all
	files := make([]spec.File, len(src))
	var total int64
	entries := make([]payload.Entry, 0, len(src)+2)
	for i, f := range src {
		entries = append(entries, hashed(f, &files[i]))
		total += f.Size
	}
	entries = append(entries, payload.Bytes("app.ico", ico))
	var manifest *spec.Manifest
	entries = append(entries, payload.Entry{Name: "setup.json", Open: func() (io.ReadCloser, error) {
		// written last, so every file has been read (and hashed) by now
		manifest = s.Manifest(files, license, app.Name+" "+app.Version)
		if err := manifest.Check(); err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(manifest.JSON())), nil
	}})
	logf("Packing %s (%s)…", count(len(src), "file"), human(total))
	// a 1 MiB buffer keeps the writes to disk large; h sees the same bytes, so the result's SHA-256
	// needs no second read of the file
	w := bufio.NewWriterSize(tmp, 1<<20)
	h := sha256.New()
	err = payload.Write(io.MultiWriter(w, h), prog, payload.Setup, entries)
	if err == nil {
		err = w.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Result{}, err
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return Result{}, fmt.Errorf("%w (is %s running?)", err, filepath.Base(out))
	}
	info, err := os.Stat(out)
	if err != nil {
		return Result{}, err
	}
	// Bytes comes from the manifest: the sizes actually read, which may differ from what
	// spec.Sources saw if a file changed meanwhile
	r := Result{Path: out, Files: len(src), Bytes: manifest.Size(), Size: info.Size(), SHA256: hex.EncodeToString(h.Sum(nil))}
	logf("Wrote %s (%s), SHA-256 %s", out, human(r.Size), r.SHA256)
	return r, nil
}

// iconFor: the spec's icon, else the packed program's own icon, else the maker's.
// ico is a whole .ico file; from says where it came from, for the log. The packed program is looked
// for only when run.program is an .exe among the packed files; any trouble reading its icon falls
// through to the maker's. An error means the spec's icon file cannot be read, or no icon was found.
func iconFor(s *spec.Spec, stub []byte) (ico []byte, from string, err error) {
	if s.Icon != "" {
		b, err := os.ReadFile(s.Abs(s.Icon))
		if err != nil {
			return nil, "", fmt.Errorf("icon: %w", err)
		}
		return b, s.Icon, nil
	}
	if p := s.Run.Program; p != spec.Python && strings.EqualFold(filepath.Ext(p), ".exe") {
		if src, err := s.Sources(); err == nil {
			for _, f := range src {
				if !strings.EqualFold(f.Path, strings.Trim(filepath.ToSlash(p), "/")) {
					continue
				}
				if exe, err := os.ReadFile(f.Abs); err == nil {
					if res, err := winres.Resources(exe); err == nil {
						if ico, err := winres.ICOFromResources(res); err == nil {
							return ico, "the icon of " + f.Path, nil
						}
					}
				}
			}
		}
	}
	res, err := winres.Resources(stub)
	if err != nil {
		return nil, "", err
	}
	ico, err = winres.ICOFromResources(res)
	if err != nil {
		return nil, "", errors.New("icon: none given, and neither the program nor the maker has one")
	}
	return ico, "the wizard maker's own (none given)", nil
}

// setupProgram is the maker's exe with the program's icon and version info, keeping the maker's manifest.
// The resulting resources are: the icon as RT_ICON images under RT_GROUP_ICON 1 (the group Explorer
// shows), the maker's RT_MANIFEST (which asks for Common Controls 6), and a fresh
// VS_VERSIONINFO, which Explorer's file properties show. Everything else of the maker's goes.
// winres.SetResources also drops the stub's own payload, if any.
func setupProgram(s *spec.Spec, stub []byte, images []winres.IconImage) ([]byte, error) {
	old, err := winres.Resources(stub)
	if err != nil {
		return nil, err
	}
	res := winres.IconResources(images, 1)
	// keep the manifest, which has a numeric type (TypeName empty); without it the setup's
	// controls would lose the modern look
	for _, r := range old {
		if r.Type == winres.TypeManifest && r.TypeName == "" {
			res = append(res, r)
		}
	}
	res = append(res, winres.VersionInfo{Version: winres.ParseVersion(s.Version), Strings: [][2]string{
		{"CompanyName", s.Publisher},
		{"FileDescription", s.Name + " Setup"},
		{"FileVersion", s.Version},
		{"InternalName", "setup"},
		{"ProductName", s.Name},
		{"ProductVersion", s.Version},
		{"Comments", "Made with " + app.Name + " " + app.Version},
	}}.Resource())
	return winres.SetResources(stub, res)
}

// hashed packs a file, recording its size and SHA-256 as it is read.
// The file is opened only when payload.Write reaches the entry, and *out is set when Write closes it,
// so each file is read once, both for the zip and for its hash.
func hashed(f spec.Source, out *spec.File) payload.Entry {
	return payload.Entry{Name: "files/" + f.Path, Open: func() (io.ReadCloser, error) {
		r, err := os.Open(f.Abs)
		if err != nil {
			return nil, err
		}
		return &hashReader{r: r, h: sha256.New(), done: func(n int64, sum string) {
			*out = spec.File{Path: f.Path, Size: n, SHA256: sum}
		}}, nil
	}}
}

// hashReader passes a file's bytes through while counting and hashing them.
type hashReader struct {
	r    io.ReadCloser
	h    hash.Hash
	n    int64                     // bytes read so far
	done func(n int64, sum string) // called on Close with the count and the hex SHA-256
}

// Read reads from the file and feeds what it got to the hash (only p[:n], the bytes actually read).
func (h *hashReader) Read(p []byte) (int, error) {
	n, err := h.r.Read(p)
	h.h.Write(p[:n])
	h.n += int64(n)
	return n, err
}

// Close reports the size and hash through done, then closes the file.
func (h *hashReader) Close() error {
	h.done(h.n, hex.EncodeToString(h.h.Sum(nil)))
	return h.r.Close()
}

// count is "1 file", "3 files".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// human is a size for people: 512 bytes, 3.4 KB, 12.0 MB. KB and MB are 1024-based here.
func human(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=4767e3dd98973c14ddc6ed44f2cb0146054b16648204b88d0fdcb9fd65eb28ee ====
