package maker

import (
	"path/filepath"
	"reflect"
	"testing"

	"acme/wizard/internal/spec"
)

func TestFormRoundTrip(t *testing.T) {
	s := spec.New(`C:\work\hello`)
	s.ID, s.Name, s.Version, s.Publisher, s.Group = "ACME.Hello", "ACME Hello", "1.0.0", "ACME Inc. (fictional)", "ACME Inc"
	s.Files = []spec.FileRule{{From: "hello.exe"}, {From: "data", To: "data"}}
	s.Exclude = []string{"*.pyc", "__pycache__"}
	s.Run = spec.Run{Program: "hello.exe", Args: []string{"--db", `C:\a b\acme.db`}, AppUserModelID: "ACME.Hello"}
	s.Requires.Python = "3.10"
	s.Purge = []string{`{localappdata}\ACME Inc\ACME.Hello`}
	f := FormFrom(s)
	if f.Args != `--db "C:\a b\acme.db"` || f.Exclude != "*.pyc __pycache__" {
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
