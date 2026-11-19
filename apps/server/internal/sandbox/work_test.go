package sandbox

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestStageWork(t *testing.T) {
	dir := t.TempDir()
	var chowned []string
	job := Job{
		Language: LangPython,
		Code:     []byte("print('hi')\n"),
		Files:    []File{{Name: "a.csv", Data: []byte("x,y\n1,2\n")}, {Name: "b.bin", Data: []byte{0, 1, 2}}},
	}
	err := StageWork(dir, job, func(p string) error { chowned = append(chowned, p); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "main.py")); string(b) != "print('hi')\n" {
		t.Errorf("main.py = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "in", "b.bin")); !bytes.Equal(b, []byte{0, 1, 2}) {
		t.Errorf("in/b.bin = %v", b)
	}
	if st, err := os.Stat(filepath.Join(dir, "out")); err != nil || !st.IsDir() {
		t.Errorf("out/ missing: %v", err)
	}
	if len(chowned) != 5 { // in, out, main.py, a.csv, b.bin
		t.Errorf("chowned %d paths: %v", len(chowned), chowned)
	}
}

func TestStageWorkRejectsUncleanNames(t *testing.T) {
	for _, name := range []string{"../x", "a/b", "", "dup"} {
		dir := t.TempDir()
		files := []File{{Name: name, Data: []byte("x")}}
		if name == "dup" {
			files = append(files, File{Name: "dup", Data: []byte("y")})
		}
		if err := StageWork(dir, Job{Language: LangBash, Code: []byte("true"), Files: files}, nil); err == nil {
			t.Errorf("StageWork with %q = nil error", name)
		}
	}
}

func TestSeedDir(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "fontlist-v390.json"), []byte(`{"fonts":[]}`), 0o644)
	os.Mkdir(filepath.Join(src, "sub"), 0o755)
	os.Symlink("/etc/passwd", filepath.Join(src, "link"))
	var chowned int
	if err := SeedDir(src, dst, func(string) error { chowned++; return nil }); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dst)
	if len(entries) != 1 || entries[0].Name() != "fontlist-v390.json" || chowned != 1 {
		t.Errorf("seeded %v, chowned %d", entries, chowned)
	}
	if err := SeedDir(filepath.Join(src, "missing"), dst, nil); err != nil {
		t.Errorf("missing src: %v", err)
	}
}

func TestCollectOutputs(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o755)
	for i := range 12 {
		os.WriteFile(filepath.Join(out, fmt.Sprintf("f%02d.txt", i)), []byte(fmt.Sprint(i)), 0o644)
	}
	os.WriteFile(filepath.Join(out, "big.bin"), make([]byte, MaxOutputFileBytes+1), 0o644)
	os.Mkdir(filepath.Join(out, "sub"), 0o755)
	os.WriteFile(filepath.Join(out, "sub", "deep.txt"), []byte("no"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret"), []byte("host file"), 0o644)
	os.Symlink(filepath.Join(dir, "secret"), filepath.Join(out, "a-link"))
	os.WriteFile(filepath.Join(out, "bad\nname"), []byte("x"), 0o644)

	files, dropped, err := CollectOutputs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != MaxOutputFiles {
		t.Fatalf("got %d files, want %d", len(files), MaxOutputFiles)
	}
	if files[0].Name != "f00.txt" || string(files[9].Data) != "9" {
		t.Errorf("files = %s .. %s", files[0].Name, files[9].Name)
	}
	reasons := map[string]string{}
	for _, d := range dropped {
		reasons[d.Name] = d.Reason
	}
	want := map[string]string{
		"a-link":   DropNotRegular,
		"big.bin":  DropTooLarge,
		"sub":      DropDirectory,
		"f10.txt":  DropTooMany,
		"f11.txt":  DropTooMany,
		"bad?name": DropBadName,
	}
	for name, reason := range want {
		if reasons[name] != reason {
			t.Errorf("dropped[%q] = %q, want %q", name, reasons[name], reason)
		}
	}
	for _, f := range files {
		if string(f.Data) == "host file" {
			t.Error("followed a symlink out of /work/out")
		}
	}
}

func TestCollectOutputsMissingOrReplaced(t *testing.T) {
	dir := t.TempDir()
	files, dropped, err := CollectOutputs(dir)
	if err != nil || files != nil || dropped != nil {
		t.Errorf("no out dir: %v %v %v", files, dropped, err)
	}
	os.WriteFile(filepath.Join(dir, "out"), []byte("x"), 0o644)
	_, dropped, err = CollectOutputs(dir)
	if err != nil || len(dropped) != 1 || dropped[0].Reason != DropOutMissing {
		t.Errorf("out is a file: %v %v", dropped, err)
	}
	dir2 := t.TempDir()
	os.Symlink("/", filepath.Join(dir2, "out"))
	files, dropped, err = CollectOutputs(dir2)
	if err != nil || len(files) != 0 || len(dropped) != 1 {
		t.Errorf("out is a symlink to /: files=%d dropped=%v err=%v", len(files), dropped, err)
	}
}

// A sandbox process that is still alive while the outputs are read can change
// a path between the check on it and the open that follows. raceHook stands
// in for that process. Whatever it swaps in, nothing outside the work dir may
// come back.
func TestCollectOutputsRaces(t *testing.T) {
	setup := func(t *testing.T) (dir, host string) {
		host = t.TempDir() // stands in for a host dir such as /etc/kandang
		os.WriteFile(filepath.Join(host, "token"), []byte("host secret"), 0o600)
		dir = t.TempDir()
		os.Mkdir(filepath.Join(dir, "out"), 0o755)
		os.WriteFile(filepath.Join(dir, "out", "token"), []byte("sandbox file"), 0o644)
		t.Cleanup(func() { raceHook = nil })
		return dir, host
	}
	noSecret := func(t *testing.T, files []File) {
		t.Helper()
		for _, f := range files {
			if string(f.Data) == "host secret" {
				t.Fatalf("a host file came back as output %q", f.Name)
			}
		}
	}

	t.Run("out becomes a symlink to a host dir", func(t *testing.T) {
		dir, host := setup(t)
		raceHook = func(stage string) {
			if stage == "out" {
				os.Rename(filepath.Join(dir, "out"), filepath.Join(dir, "parked"))
				os.Symlink(host, filepath.Join(dir, "out"))
			}
		}
		files, dropped, err := CollectOutputs(dir)
		noSecret(t, files)
		if err != nil || len(files) != 0 || len(dropped) != 1 || dropped[0].Reason != DropOutMissing {
			t.Errorf("files=%v dropped=%v err=%v", files, dropped, err)
		}
	})

	t.Run("a file becomes a symlink to a host file", func(t *testing.T) {
		dir, host := setup(t)
		raceHook = func(stage string) {
			if stage == "file" {
				os.Remove(filepath.Join(dir, "out", "token"))
				os.Symlink(filepath.Join(host, "token"), filepath.Join(dir, "out", "token"))
			}
		}
		files, dropped, err := CollectOutputs(dir)
		noSecret(t, files)
		if err != nil || len(files) != 0 || len(dropped) != 1 || dropped[0].Reason != DropChanged {
			t.Errorf("files=%v dropped=%v err=%v", files, dropped, err)
		}
	})

	t.Run("a file becomes a fifo", func(t *testing.T) {
		dir, _ := setup(t)
		raceHook = func(stage string) {
			if stage == "file" {
				os.Remove(filepath.Join(dir, "out", "token"))
				if err := syscall.Mkfifo(filepath.Join(dir, "out", "token"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		// A fifo with no writer must neither block the read nor come back.
		files, dropped, err := CollectOutputs(dir)
		if err != nil || len(files) != 0 || len(dropped) != 1 || dropped[0].Reason != DropNotRegular {
			t.Errorf("files=%v dropped=%v err=%v", files, dropped, err)
		}
	})
}

// The code in the sandbox decides how many entries /work/out has. A run that
// leaves thousands must not turn into a listing of thousands.
func TestCollectOutputsManyEntries(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o755)
	for i := range MaxOutputScan + 500 {
		if err := os.Mkdir(filepath.Join(out, fmt.Sprintf("d%05d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files, dropped, err := CollectOutputs(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
	if len(dropped) != MaxDroppedListed+2 {
		t.Fatalf("dropped has %d entries, want %d named and two notes", len(dropped), MaxDroppedListed)
	}
	more, scan := dropped[MaxDroppedListed], dropped[MaxDroppedListed+1]
	if more.Reason != fmt.Sprintf(DropMoreFormat, MaxOutputScan-MaxDroppedListed) || scan.Reason != DropTooManyEntries {
		t.Errorf("notes = %+v, %+v", more, scan)
	}

	// Exactly at the scan limit nothing is cut.
	dir2 := t.TempDir()
	os.Mkdir(filepath.Join(dir2, "out"), 0o755)
	for i := range MaxDroppedListed {
		os.Mkdir(filepath.Join(dir2, "out", fmt.Sprintf("d%03d", i)), 0o755)
	}
	os.WriteFile(filepath.Join(dir2, "out", "a.txt"), []byte("kept"), 0o644)
	files, dropped, err = CollectOutputs(dir2)
	if err != nil || len(files) != 1 || len(dropped) != MaxDroppedListed {
		t.Errorf("files=%d dropped=%d err=%v", len(files), len(dropped), err)
	}
}
