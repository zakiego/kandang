package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readTrim(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestCgroupSetupAndCreate(t *testing.T) {
	root := t.TempDir()
	c := Cgroups{Root: root}
	if err := c.Setup(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, filepath.Join(root, "kandang")} {
		if got := readTrim(t, filepath.Join(dir, "cgroup.subtree_control")); got != "+memory +pids +cpu" {
			t.Errorf("%s subtree_control = %q", dir, got)
		}
	}

	id := NewID()
	g, err := c.Create(id, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if g.Path != filepath.Join(root, "kandang", id) {
		t.Errorf("path = %s", g.Path)
	}
	want := map[string]string{
		"memory.max":       "1073741824",
		"memory.swap.max":  "0",
		"memory.oom.group": "1",
		"pids.max":         "256",
		"cpu.max":          "100000 100000",
	}
	for f, v := range want {
		if got := readTrim(t, filepath.Join(g.Path, f)); got != v {
			t.Errorf("%s = %q, want %q", f, got, v)
		}
	}

	// The kernel provides these; stand in for it.
	os.WriteFile(filepath.Join(g.Path, "memory.peak"), []byte("236978176\n"), 0o644)
	os.WriteFile(filepath.Join(g.Path, "memory.events"), []byte("low 0\nhigh 0\nmax 12\noom 1\noom_kill 1\noom_group_kill 1\n"), 0o644)
	os.WriteFile(filepath.Join(g.Path, "cgroup.events"), []byte("populated 0\nfrozen 0\n"), 0o644)
	if got := g.Peak(); got != 236978176 {
		t.Errorf("Peak = %d", got)
	}
	if got := g.OOMKills(); got != 1 {
		t.Errorf("OOMKills = %d", got)
	}
	if g.Populated() {
		t.Error("Populated = true")
	}
	if g.PidsLimited() {
		t.Error("PidsLimited = true with no pids.events")
	}
	os.WriteFile(filepath.Join(g.Path, "pids.events"), []byte("max 3\n"), 0o644)
	if !g.PidsLimited() {
		t.Error("PidsLimited = false with max 3")
	}

	os.WriteFile(filepath.Join(g.Path, "cgroup.kill"), nil, 0o644)
	if err := g.Kill(); err != nil {
		t.Fatal(err)
	}
	if got := readTrim(t, filepath.Join(g.Path, "cgroup.kill")); got != "1" {
		t.Errorf("cgroup.kill = %q", got)
	}

	left, err := c.Leftovers()
	if err != nil || len(left) != 1 || left[0].Path != g.Path {
		t.Fatalf("Leftovers = %v, %v", left, err)
	}
}

func TestCgroupCreateRejectsBadID(t *testing.T) {
	c := Cgroups{Root: t.TempDir()}
	c.Setup()
	for _, id := range []string{"", "..", "../etc", "a/b", strings.Repeat("a", 23)} {
		if _, err := c.Create(id, DefaultLimits()); err != ErrBadID {
			t.Errorf("Create(%q) = %v, want ErrBadID", id, err)
		}
	}
}

func TestCgroupRemove(t *testing.T) {
	// On cgroupfs an rmdir works with the control files still listed; in a
	// temp dir it fails while files exist, so test both outcomes.
	c := Cgroups{Root: t.TempDir()}
	c.Setup()
	g, err := c.Create(NewID(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Remove(50 * time.Millisecond); err == nil {
		t.Error("Remove of a non-empty plain dir should time out with an error")
	}

	empty := &Cgroup{Path: filepath.Join(c.Parent(), NewID())}
	if err := os.Mkdir(empty.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := empty.Remove(time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(empty.Path); !os.IsNotExist(err) {
		t.Error("cgroup dir still there")
	}
	gone := &Cgroup{Path: filepath.Join(c.Parent(), NewID())}
	if err := gone.Remove(time.Second); err != nil {
		t.Errorf("Remove of a missing cgroup = %v", err)
	}
}

func TestCgroupCheck(t *testing.T) {
	root := t.TempDir()
	c := Cgroups{Root: root}
	if err := c.Check(); err == nil {
		t.Error("Check with no cgroup.controllers = nil")
	}
	os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("cpuset cpu io memory\n"), 0o644)
	if err := c.Check(); err == nil || !strings.Contains(err.Error(), "pids") {
		t.Errorf("Check = %v, want pids missing", err)
	}
	os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("cpuset cpu io memory hugetlb pids rdma misc\n"), 0o644)
	if err := c.Check(); err != nil {
		t.Errorf("Check = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "kandang")); !os.IsNotExist(err) {
		t.Error("Check created the parent; it must only read")
	}
}
