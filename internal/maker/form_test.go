// ==== Generic Company Inc. portfolio ====================================
// Project: install_wizard_maker | File: internal/maker/form_test.go
// About: ==== Generic Company Inc. portfolio ===============================================...
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

// ==== Generic Company Inc. portfolio ===============================================
// Project: install_wizard_maker | File: internal/maker/form_test.go
// About: Tests for package maker.
// Copyright (c) 2026 kjFriday | SPDX-License-Identifier: AGPL-3.0-only
// You may change this file under the AGPL-3.0. The stamp at the end marks
// the version kjFriday wrote. Verify with the portfolio's stamp.py --check.
// Reused code, if any, is mapped in PROVENANCE.md.
// ========================================================================

package maker

import (
	"path/filepath"
	"reflect"
	"testing"

	"generic-company/wizard/internal/spec"
)

func TestFormRoundTrip(t *testing.T) {
	s := spec.New(`C:\work\hello`)
	s.ID, s.Name, s.Version, s.Publisher, s.Group = "GenericCompany.Hello", "Generic Company Hello", "1.0.0", "Generic Company Inc. (fictional)", "Generic Company Inc"
	s.Files = []spec.FileRule{{From: "hello.exe"}, {From: "data", To: "data"}}
	s.Exclude = []string{"*.pyc", "__pycache__"}
	s.Run = spec.Run{Program: "hello.exe", Args: []string{"--db", `C:\a b\generic_company.db`}, AppUserModelID: "GenericCompany.Hello"}
	s.Requires.Python = "3.10"
	s.Purge = []string{`{localappdata}\Generic Company Inc\GenericCompany.Hello`}
	f := FormFrom(s)
	if f.Args != `--db "C:\a b\generic_company.db"` || f.Exclude != "*.pyc __pycache__" {
		t.Fatalf("form %+v", f)
	}
	back := spec.New(s.Dir())
	back.Run.AppUserModelID, back.Requires, back.Purge = s.Run.AppUserModelID, s.Requires, s.Purge
	f.Apply(back)
	if !reflect.DeepEqual(back, s) {
		t.Fatalf("round trip\n%+v\n%+v", back, s)
	}
	f.Exclude = "a, b;c  d"
	f.Apply(back)
	if !reflect.DeepEqual(back.Exclude, []string{"a", "b", "c", "d"}) {
		t.Fatalf("exclude %v", back.Exclude)
	}
}

func TestArgs(t *testing.T) {
	for line, want := range map[string][]string{
		``:                       nil,
		`-version`:               {"-version"},
		`--db "C:\a b\x.db"  -v`: {"--db", `C:\a b\x.db`, "-v"},
		`"" x`:                   {"", "x"},
		`{app}\main.py`:          {`{app}\main.py`},
	} {
		got := SplitArgs(line)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %q", line, got)
		}
		if again := SplitArgs(JoinArgs(got)); !reflect.DeepEqual(again, want) {
			t.Errorf("%q: join/split %q", line, again)
		}
	}
}

func TestRelativize(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project")
	s := spec.New(dir)
	s.Icon = filepath.Join(dir, "icon", "app.ico")
	s.License = filepath.Join(filepath.Dir(dir), "LICENSE") // outside: stays absolute
	s.Files = []spec.FileRule{{From: filepath.Join(dir, "dist")}, {From: "README.md"}}
	Relativize(s, dir)
	if s.Icon != "icon/app.ico" || s.License != filepath.Join(filepath.Dir(dir), "LICENSE") ||
		s.Files[0].From != "dist" || s.Files[1].From != "README.md" {
		t.Fatalf("%+v", s)
	}
	Absolutize(s)
	if s.Icon != filepath.Join(dir, "icon", "app.ico") || s.Files[1].From != filepath.Join(dir, "README.md") {
		t.Fatalf("%+v", s)
	}
}

// ==== stamp v1 | author=kjFriday | hmac-sha256=1693ab26580c3c2a0936f2a2216b312a00fdfe4d8d463d19b080581e8506bc0b ====
