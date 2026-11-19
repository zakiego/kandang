package sandbox

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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
