package sandbox

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Controllers kandang uses. The device controller is left out on purpose:
// an isolated OrbStack machine blocks it.
var Controllers = []string{"memory", "pids", "cpu"}

// Cgroups manages <Root>/kandang/<runID> on a cgroup v2 hierarchy. Root is
// /sys/fs/cgroup in production and a temp dir in tests.
type Cgroups struct {
	Root string
}

// Parent is the directory every run cgroup lives under.
func (c Cgroups) Parent() string {
	return filepath.Join(c.Root, "kandang")
}

// Setup creates the parent and enables the controllers for its children, in
// the root and in the parent.
func (c Cgroups) Setup() error {
	enable := "+" + strings.Join(Controllers, " +")
	if err := os.MkdirAll(c.Parent(), 0o755); err != nil {
		return fmt.Errorf("cgroup: %w", err)
	}
	for _, dir := range []string{c.Root, c.Parent()} {
		if err := writeFile(filepath.Join(dir, "cgroup.subtree_control"), enable); err != nil {
			return fmt.Errorf("cgroup: enable controllers in %s: %w", dir, err)
		}
	}
	return nil
}

// Check reports whether the root has every controller kandang needs. It only
// reads.
func (c Cgroups) Check() error {
	b, err := os.ReadFile(filepath.Join(c.Root, "cgroup.controllers"))
	if err != nil {
		return fmt.Errorf("cgroup v2 not found at %s: %w", c.Root, err)
	}
	have := map[string]bool{}
	for _, f := range strings.Fields(string(b)) {
		have[f] = true
	}
	var missing []string
	for _, want := range Controllers {
		if !have[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("cgroup controllers missing at %s: %s", c.Root, strings.Join(missing, ", "))
	}
	return nil
}

// Cgroup is one run's cgroup.
type Cgroup struct {
	Path string
}

// Create makes the run's cgroup and writes its limits: memory.max, no swap,
// pids.max, cpu.max, and memory.oom.group so an OOM kill takes the whole
// sandbox down together.
func (c Cgroups) Create(id string, l Limits) (*Cgroup, error) {
	if !ValidID(id) {
		return nil, ErrBadID
	}
	g := &Cgroup{Path: filepath.Join(c.Parent(), id)}
	if err := os.Mkdir(g.Path, 0o755); err != nil {
		return nil, fmt.Errorf("cgroup: %w", err)
	}
	writes := []struct{ file, value string }{
		{"memory.max", strconv.FormatInt(l.MemoryBytes, 10)},
		{"memory.swap.max", "0"},
		{"memory.oom.group", "1"},
		{"pids.max", strconv.Itoa(l.Pids)},
		{"cpu.max", l.CPUMax},
	}
	for _, w := range writes {
		if err := writeFile(filepath.Join(g.Path, w.file), w.value); err != nil {
			os.Remove(g.Path)
			return nil, fmt.Errorf("cgroup: write %s: %w", w.file, err)
		}
	}
	return g, nil
}

// Peak returns memory.peak in bytes, or 0 when the kernel doesn't have it.
func (g *Cgroup) Peak() int64 {
	b, err := os.ReadFile(filepath.Join(g.Path, "memory.peak"))
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return n
}

// OOMKills returns the oom_kill count from memory.events.
func (g *Cgroup) OOMKills() int {
	return eventCount(filepath.Join(g.Path, "memory.events"), "oom_kill")
}

// PidsLimited reports whether the cgroup ever refused a new task because of
// pids.max (the "max" count in pids.events).
func (g *Cgroup) PidsLimited() bool {
	return eventCount(filepath.Join(g.Path, "pids.events"), "max") > 0
}

// Populated reports whether any process is still in the cgroup.
func (g *Cgroup) Populated() bool {
	return eventCount(filepath.Join(g.Path, "cgroup.events"), "populated") > 0
}

// Kill sends SIGKILL to every process in the cgroup. cgroup.kill is a
// kernel file; it is never created.
func (g *Cgroup) Kill() error {
	f, err := os.OpenFile(filepath.Join(g.Path, "cgroup.kill"), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, werr := f.WriteString("1")
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// KillAndWait kills every process in the cgroup and waits up to wait until
// none is left. After it returns nil, nothing from the run can still touch
// its files. A cgroup that is already gone counts as empty.
func (g *Cgroup) KillAndWait(wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		if err := g.Kill(); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if !g.Populated() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("cgroup: %s still has processes %s after the kill", g.Path, wait)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Remove kills what's left and removes the cgroup, waiting up to wait for the
// processes to go.
func (g *Cgroup) Remove(wait time.Duration) error {
	_ = g.Kill() // a missing cgroup shows up as ENOENT from the rmdir below
	deadline := time.Now().Add(wait)
	for {
		err := os.Remove(g.Path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("cgroup: remove %s: %w", g.Path, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Leftovers lists run cgroups under the parent, from an earlier crash.
func (c Cgroups) Leftovers() ([]*Cgroup, error) {
	entries, err := os.ReadDir(c.Parent())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Cgroup
	for _, e := range entries {
		if e.IsDir() && ValidID(e.Name()) {
			out = append(out, &Cgroup{Path: filepath.Join(c.Parent(), e.Name())})
		}
	}
	return out, nil
}

func eventCount(file, key string) int {
	b, err := os.ReadFile(file)
	if err != nil {
		return 0
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && f[0] == key {
			n, _ := strconv.Atoi(f[1])
			return n
		}
	}
	return 0
}

// writeFile writes one value the way cgroupfs wants it: a single write to an
// existing or new file, no temp file and rename.
func writeFile(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(value)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}
