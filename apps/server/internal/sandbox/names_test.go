package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestCleanName(t *testing.T) {
	ok := map[string]string{
		"data.csv":               "data.csv",
		"in/data.csv":            "data.csv",
		"/etc/passwd":            "passwd",
		`C:\Users\x\q1.xlsx`:     "q1.xlsx",
		"laporan bulan 3.pdf":    "laporan bulan 3.pdf",
		"foto-ü.jpg":             "foto-ü.jpg",
		".hidden":                ".hidden",
		"a..b.txt":               "a..b.txt",
		"dir/":                   "dir",
		strings.Repeat("a", 128): strings.Repeat("a", 128),
	}
	for in, want := range ok {
		got, err := CleanName(in)
		if err != nil || got != want {
			t.Errorf("CleanName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := map[string]error{
		"":                       ErrNameEmpty,
		"/":                      ErrNameEmpty,
		".":                      ErrNameEmpty,
		"  ":                     ErrNameEmpty,
		"..":                     ErrNameDotDot,
		"../x":                   ErrNameDotDot,
		"a/../b":                 ErrNameDotDot,
		`..\x`:                   ErrNameDotDot,
		"a\x00b":                 ErrNameControl,
		"a\nb":                   ErrNameControl,
		"a\tb":                   ErrNameControl,
		"a\x7fb":                 ErrNameControl,
		"a\u0085b":               ErrNameControl,
		"\xff.txt":               ErrNameUTF8,
		strings.Repeat("a", 129): ErrNameLong,
		strings.Repeat("é", 65):  ErrNameLong, // 130 bytes
	}
	for in, want := range bad {
		if got, err := CleanName(in); err != want {
			t.Errorf("CleanName(%q) = %q, %v; want %v", in, got, err, want)
		}
	}
}

func TestIDs(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := NewID()
		if !ValidID(id) {
			t.Fatalf("NewID() = %q isn't valid", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
	for _, id := range []string{"", "short", "../../../../../../etc/x", strings.Repeat("a", 21) + "/", strings.Repeat("a", 21) + "."} {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true", id)
		}
	}
}

func TestLimits(t *testing.T) {
	l := DefaultLimits()
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	if l.MemoryBytes != 1<<30 || l.Pids != 256 || l.CPUMax != "100000 100000" || l.TmpBytes != 256<<20 || l.WorkBytes != 512<<20 {
		t.Errorf("defaults = %+v", l)
	}
	if l.DefaultTimeout != 60*time.Second || l.MaxTimeout != 300*time.Second {
		t.Errorf("timeouts = %s, %s", l.DefaultTimeout, l.MaxTimeout)
	}
	for _, mut := range []func(*Limits){
		func(l *Limits) { l.MemoryBytes = 1 << 20 },
		func(l *Limits) { l.Pids = 8 },
		func(l *Limits) { l.Procs = 0 },
		func(l *Limits) { l.Procs = 200 },
		func(l *Limits) { l.CPUMax = "" },
		func(l *Limits) { l.WorkBytes = 10 << 20 },
		func(l *Limits) { l.MaxTimeout = time.Second },
	} {
		c := DefaultLimits()
		mut(&c)
		if c.Validate() == nil {
			t.Errorf("Validate(%+v) = nil", c)
		}
	}
}

func TestCommand(t *testing.T) {
	if _, err := Command("ruby"); err == nil {
		t.Error("Command(ruby) = nil error")
	}
	if got, _ := ScriptName(LangBash); got != "main.sh" {
		t.Errorf("ScriptName(bash) = %s", got)
	}
}

func TestParseRunscVersion(t *testing.T) {
	out := "runsc version release-20261116.0\nspec: 1.1.0-rc.1\n"
	if got := ParseRunscVersion(out); got != "release-20261116.0" {
		t.Errorf("got %q", got)
	}
}
