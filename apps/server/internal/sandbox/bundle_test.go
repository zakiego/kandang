package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func decodeBundle(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("config.json isn't JSON: %v", err)
	}
	return m
}

func get(t *testing.T, m any, path ...any) any {
	t.Helper()
	cur := m
	for _, p := range path {
		switch k := p.(type) {
		case string:
			obj, ok := cur.(map[string]any)
			if !ok {
				t.Fatalf("at %v: not an object", path)
			}
			cur, ok = obj[k]
			if !ok {
				t.Fatalf("missing %v", path)
			}
		case int:
			arr, ok := cur.([]any)
			if !ok || k >= len(arr) {
				t.Fatalf("at %v: not an array or too short", path)
			}
			cur = arr[k]
		}
	}
	return cur
}

func TestBundlePython(t *testing.T) {
	b, err := Bundle(BundleSpec{Rootfs: "/var/lib/kandang/rootfs", WorkHost: "/var/lib/kandang/state/runs/x/work", Language: LangPython, Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	m := decodeBundle(t, b)

	if got := get(t, m, "process", "args"); !reflect.DeepEqual(got, []any{"python3", "/work/main.py"}) {
		t.Errorf("args = %v", got)
	}
	if got := get(t, m, "process", "cwd"); got != "/work" {
		t.Errorf("cwd = %v", got)
	}
	if uid, gid := get(t, m, "process", "user", "uid"), get(t, m, "process", "user", "gid"); uid != 10001.0 || gid != 10001.0 {
		t.Errorf("user = %v:%v, want 10001:10001", uid, gid)
	}
	if get(t, m, "process", "noNewPrivileges") != true {
		t.Error("noNewPrivileges isn't true")
	}
	if get(t, m, "process", "terminal") != false {
		t.Error("terminal isn't false")
	}
	for _, set := range []string{"bounding", "effective", "inheritable", "permitted", "ambient"} {
		v := get(t, m, "process", "capabilities", set)
		arr, ok := v.([]any)
		if !ok || len(arr) != 0 {
			t.Errorf("capabilities.%s = %v, want an empty list (not null)", set, v)
		}
	}
	env := get(t, m, "process", "env").([]any)
	wantEnv := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/tmp", "PYTHONDONTWRITEBYTECODE=1", "MPLCONFIGDIR=/opt/mpl"}
	for _, w := range wantEnv {
		found := false
		for _, e := range env {
			if e == w {
				found = true
			}
		}
		if !found {
			t.Errorf("env is missing %s: %v", w, env)
		}
	}
	rl := get(t, m, "process", "rlimits").([]any)
	if len(rl) != 2 || get(t, rl, 0, "type") != "RLIMIT_NOFILE" || get(t, rl, 0, "hard") != 1024.0 || get(t, rl, 0, "soft") != 1024.0 {
		t.Errorf("rlimits = %v", rl)
	}
	if get(t, rl, 1, "type") != "RLIMIT_NPROC" || get(t, rl, 1, "hard") != 64.0 || get(t, rl, 1, "soft") != 64.0 {
		t.Errorf("RLIMIT_NPROC = %v", rl[1])
	}
	if get(t, m, "root", "readonly") != true || get(t, m, "root", "path") != "/var/lib/kandang/rootfs" {
		t.Errorf("root = %v", get(t, m, "root"))
	}
	if get(t, m, "hostname") != "kandang" {
		t.Errorf("hostname = %v", get(t, m, "hostname"))
	}

	var ns []string
	for _, n := range get(t, m, "linux", "namespaces").([]any) {
		ns = append(ns, n.(map[string]any)["type"].(string))
	}
	if !reflect.DeepEqual(ns, []string{"pid", "network", "ipc", "uts", "mount"}) {
		t.Errorf("namespaces = %v", ns)
	}
	if _, ok := get(t, m, "linux").(map[string]any)["resources"]; ok {
		t.Error("linux.resources is set; limits belong to kandang's own cgroup (runsc --ignore-cgroups)")
	}

	mounts := map[string]map[string]any{}
	for _, mt := range get(t, m, "mounts").([]any) {
		mm := mt.(map[string]any)
		mounts[mm["destination"].(string)] = mm
	}
	tmp := mounts["/tmp"]
	if tmp == nil || tmp["type"] != "tmpfs" || !hasOpt(tmp, "size=268435456") || !hasOpt(tmp, "nosuid") || !hasOpt(tmp, "nodev") {
		t.Errorf("/tmp mount = %v", tmp)
	}
	work := mounts["/work"]
	if work == nil || work["type"] != "bind" || work["source"] != "/var/lib/kandang/state/runs/x/work" || !hasOpt(work, "rw") || !hasOpt(work, "nosuid") {
		t.Errorf("/work mount = %v", work)
	}
	if mounts["/proc"] == nil || mounts["/dev"] == nil {
		t.Error("missing /proc or /dev")
	}
	if len(mounts) != 4 {
		t.Errorf("want exactly 4 mounts, got %d: nothing from the host but /work", len(mounts))
	}
}

func hasOpt(m map[string]any, opt string) bool {
	opts, _ := m["options"].([]any)
	for _, o := range opts {
		if o == opt {
			return true
		}
	}
	return false
}

func TestBundleBash(t *testing.T) {
	b, err := Bundle(BundleSpec{Rootfs: "/r", WorkHost: "/w", Language: LangBash, Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if got := get(t, decodeBundle(t, b), "process", "args"); !reflect.DeepEqual(got, []any{"bash", "/work/main.sh"}) {
		t.Errorf("args = %v", got)
	}
}

func TestBundleMplMount(t *testing.T) {
	b, err := Bundle(BundleSpec{Rootfs: "/r", WorkHost: "/run/fs/work", MplHost: "/run/fs/mpl", Language: LangPython, Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	var mpl map[string]any
	for _, mt := range get(t, decodeBundle(t, b), "mounts").([]any) {
		if mm := mt.(map[string]any); mm["destination"] == "/opt/mpl" {
			mpl = mm
		}
	}
	if mpl == nil || mpl["type"] != "bind" || mpl["source"] != "/run/fs/mpl" || !hasOpt(mpl, "rw") || !hasOpt(mpl, "nosuid") {
		t.Errorf("/opt/mpl mount = %v", mpl)
	}
	if _, err := Bundle(BundleSpec{Rootfs: "/r", WorkHost: "/w", MplHost: "mpl", Language: LangPython}); err == nil {
		t.Error("relative MplHost accepted")
	}
}

func TestBundleRejects(t *testing.T) {
	cases := []BundleSpec{
		{Rootfs: "rootfs", WorkHost: "/w", Language: LangPython},
		{Rootfs: "/r", WorkHost: "w", Language: LangPython},
		{Rootfs: "/r", WorkHost: "/w", Language: "ruby"},
	}
	for _, c := range cases {
		if _, err := Bundle(c); err == nil {
			t.Errorf("Bundle(%+v) = nil error", c)
		}
	}
}

func TestWriteBundle(t *testing.T) {
	dir := t.TempDir()
	if err := WriteBundle(dir, BundleSpec{Rootfs: "/r", WorkHost: "/w", Language: LangPython, Limits: DefaultLimits()}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"ociVersion": "1.0.2"`) {
		t.Errorf("config.json = %s", b)
	}
}
