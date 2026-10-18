package app

import (
	"regexp"
	"strings"
	"testing"
)

func TestIdentity(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Fatalf("version %q", Version)
	}
	if !strings.HasSuffix(Title, Name+" "+Version) || !strings.HasPrefix(Title, "ACME Inc. — ") {
		t.Fatalf("title %q", Title)
	}
	if !strings.Contains(Credit, "CC BY 2.5") {
		t.Fatal("icon credit missing")
	}
}
