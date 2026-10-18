// Package maker is the wizard maker itself: Build turns a spec into a setup.exe, Main is its command
// line and window.
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

	"acme/wizard/internal/app"
	"acme/wizard/internal/payload"
	"acme/wizard/internal/spec"
	"acme/wizard/internal/winres"
)

// Result describes a setup.exe that was built.
type Result struct {
	Path   string
	Files  int   // packed files
	Bytes  int64 // their total size
	Size   int64 // the setup.exe's size
	SHA256 string
}

// SpecError means setup.json itself is wrong (as opposed to a file that could not be read or written).
type SpecError struct{ Err error }

func (e *SpecError) Error() string { return e.Err.Error() }
func (e *SpecError) Unwrap() error { return e.Err }

// Build makes the setup.exe for s. stub is the maker's own exe: a setup.exe is a copy of it with the
// program's icon and version info, then the payload. logf, if set, hears each step.
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
	out, _ := s.OutputPath()
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return Result{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".setup-*.tmp")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed

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
	r := Result{Path: out, Files: len(src), Bytes: manifest.Size(), Size: info.Size(), SHA256: hex.EncodeToString(h.Sum(nil))}
	logf("Wrote %s (%s), SHA-256 %s", out, human(r.Size), r.SHA256)
	return r, nil
}

// iconFor: the spec's icon, else the packed program's own icon, else the maker's.
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
func setupProgram(s *spec.Spec, stub []byte, images []winres.IconImage) ([]byte, error) {
	old, err := winres.Resources(stub)
	if err != nil {
		return nil, err
	}
	res := winres.IconResources(images, 1)
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

type hashReader struct {
	r    io.ReadCloser
	h    hash.Hash
	n    int64
	done func(n int64, sum string)
}

func (h *hashReader) Read(p []byte) (int, error) {
	n, err := h.r.Read(p)
	h.h.Write(p[:n])
	h.n += int64(n)
	return n, err
}

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

// human is a size for people: 512 bytes, 3.4 KB, 12.0 MB.
func human(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}
