package sandbox

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

// StageWork fills dir, the host side of /work, before a run: the script at
// the top, the input files in in/, and an empty out/. chown, when set, is
// called on every path it creates so the sandbox user owns them.
func StageWork(dir string, job Job, chown func(path string) error) error {
	script, err := ScriptName(job.Language)
	if err != nil {
		return err
	}
	if chown == nil {
		chown = func(string) error { return nil }
	}
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	for _, d := range []string{in, out} {
		if err := os.Mkdir(d, 0o755); err != nil {
			return err
		}
		if err := chown(d); err != nil {
			return err
		}
	}
	if err := writeNew(filepath.Join(dir, script), job.Code, chown); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, f := range job.Files {
		if name, err := CleanName(f.Name); err != nil || name != f.Name {
			return fmt.Errorf("input file %q: not a clean name", f.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("input file %q: duplicate name", f.Name)
		}
		seen[f.Name] = true
		if err := writeNew(filepath.Join(in, f.Name), f.Data, chown); err != nil {
			return err
		}
	}
	return nil
}

func writeNew(path string, data []byte, chown func(string) error) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return chown(path)
}

// MaxSeedFileBytes caps each file SeedDir copies.
const MaxSeedFileBytes = 8 << 20

// SeedDir copies the top-level regular files of src into dst (which must
// exist), so a run gets a writable copy of a read-only image dir. A missing
// src is fine: there's nothing to seed.
func SeedDir(src, dst string, chown func(path string) error) error {
	entries, err := os.ReadDir(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if chown == nil {
		chown = func(string) error { return nil }
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if len(data) > MaxSeedFileBytes {
			continue
		}
		if err := writeNew(filepath.Join(dst, e.Name()), data, chown); err != nil {
			return err
		}
	}
	return nil
}

// Reasons an output file is dropped.
const (
	DropNotRegular = "not a regular file"
	DropDirectory  = "a directory; only top-level files in /work/out come back"
	DropTooLarge   = "larger than 15 MiB"
	DropTooMany    = "more than 10 files in /work/out"
	DropBadName    = "file name isn't allowed"
	DropOutMissing = "/work/out isn't a directory"
	DropChanged    = "changed while it was being read"

	DropTooManyEntries = "more than 1000 entries in /work/out; the rest were not looked at"
	DropMoreFormat     = "%d more entries were dropped; only the first 100 are listed"
)

// CollectOutputs reads the top-level regular files in dir/out after a run,
// sorted by name: at most MaxOutputFiles, each at most MaxOutputFileBytes.
// Everything else is listed as dropped with a reason.
//
// The sandbox wrote this directory and is not trusted. Every path is
// resolved inside a root at dir (the host side of /work), out included, so
// a symlink can never lead to a host file, even one swapped in while this
// runs. A link that stays inside /work is only reported as dropped.
func CollectOutputs(dir string) ([]File, []Dropped, error) {
	work, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = work.Close() }()
	st, err := work.Lstat("out")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil // the code removed it; nothing to return
	}
	if err != nil {
		return nil, nil, err
	}
	if !st.IsDir() {
		return nil, []Dropped{{Name: "out", Reason: DropOutMissing}}, nil
	}
	if raceHook != nil {
		raceHook("out")
	}
	root, err := work.OpenRoot("out")
	if err != nil {
		// It was a directory a moment ago: something replaced it.
		return nil, []Dropped{{Name: "out", Reason: DropOutMissing}}, nil
	}
	defer func() { _ = root.Close() }()
	// The sandbox can leave hundreds of thousands of entries here. Only the
	// first MaxOutputScan are looked at, so the listing can't grow kandang's
	// memory or the response.
	d, err := root.Open(".")
	if err != nil {
		return nil, nil, err
	}
	entries, err := d.ReadDir(MaxOutputScan + 1)
	_ = d.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	tooMany := len(entries) > MaxOutputScan
	if tooMany {
		entries = entries[:MaxOutputScan]
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var files []File
	var dropped []Dropped
	for _, e := range entries {
		name := e.Name()
		if clean, err := CleanName(name); err != nil || clean != name {
			dropped = append(dropped, Dropped{Name: printable(name), Reason: DropBadName})
			continue
		}
		info, err := root.Lstat(name)
		if err != nil {
			dropped = append(dropped, Dropped{Name: name, Reason: DropChanged})
			continue
		}
		switch {
		case info.IsDir():
			dropped = append(dropped, Dropped{Name: name, Reason: DropDirectory})
			continue
		case !info.Mode().IsRegular():
			dropped = append(dropped, Dropped{Name: name, Reason: DropNotRegular})
			continue
		case info.Size() > MaxOutputFileBytes:
			dropped = append(dropped, Dropped{Name: name, Reason: DropTooLarge})
			continue
		case len(files) >= MaxOutputFiles:
			dropped = append(dropped, Dropped{Name: name, Reason: DropTooMany})
			continue
		}
		if raceHook != nil {
			raceHook("file")
		}
		data, err := readCapped(root, name)
		switch {
		case errors.Is(err, errTooLarge):
			dropped = append(dropped, Dropped{Name: name, Reason: DropTooLarge})
		case errors.Is(err, errNotRegular):
			dropped = append(dropped, Dropped{Name: name, Reason: DropNotRegular})
		case err != nil:
			// Gone, replaced or pointing out of /work since the Lstat above.
			dropped = append(dropped, Dropped{Name: name, Reason: DropChanged})
		default:
			files = append(files, File{Name: name, Data: data})
		}
	}
	if extra := len(dropped) - MaxDroppedListed; extra > 0 {
		dropped = append(dropped[:MaxDroppedListed], Dropped{Name: "...", Reason: fmt.Sprintf(DropMoreFormat, extra)})
	}
	if tooMany {
		dropped = append(dropped, Dropped{Name: "...", Reason: DropTooManyEntries})
	}
	return files, dropped, nil
}

// raceHook is nil outside tests. A test sets it to change a path between the
// check on it and the open that follows, the way a sandbox process that is
// still alive could.
var raceHook func(stage string)

var (
	errTooLarge   = errors.New("too large")
	errNotRegular = errors.New("not a regular file")
)

// readCapped reads one output file. The checks that matter are made on the
// open file, not on the name: the name may point at something else by now.
func readCapped(root *os.Root, name string) ([]byte, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errNotRegular
	}
	if st.Size() > MaxOutputFileBytes {
		return nil, errTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxOutputFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxOutputFileBytes {
		return nil, errTooLarge
	}
	return data, nil
}

// printable makes a rejected name safe to echo back in JSON and logs.
func printable(name string) string {
	b := []rune{}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			r = '?'
		}
		b = append(b, r)
		if len(b) >= MaxNameBytes {
			break
		}
	}
	return string(b)
}
