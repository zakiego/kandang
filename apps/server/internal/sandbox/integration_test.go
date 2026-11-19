//go:build linux && integration

// Integration tests: real jobs through the real runner. They need root,
// runsc on PATH, cgroup v2 with memory, pids and cpu, and the rootfs
// unpacked from image/Dockerfile. Run them in a throwaway machine:
//
//	go test -c -tags integration -o kandang-it ./internal/sandbox   (GOOS=linux)
//	sudo ./kandang-it -test.v -rootfs /var/lib/kandang/rootfs
package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	itRootfs   = flag.String("rootfs", "/var/lib/kandang/rootfs", "unpacked sandbox image")
	itState    = flag.String("state", "/var/lib/kandang-it", "state dir for the test runs")
	itCgroup   = flag.String("cgroup-root", "/sys/fs/cgroup", "cgroup v2 mount")
	itRunsc    = flag.String("runsc", "runsc", "runsc binary")
	sharedOnce sync.Once
	shared     Runner
	sharedErr  error
)

func itRunner(t *testing.T) Runner {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Fatal("integration tests need root (cgroups and runsc)")
	}
	sharedOnce.Do(func() {
		shared, sharedErr = New(Config{
			Runsc: *itRunsc, Rootfs: *itRootfs, StateDir: *itState, CgroupRoot: *itCgroup,
			Limits: DefaultLimits(), Log: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		})
	})
	if sharedErr != nil {
		t.Fatal(sharedErr)
	}
	if st := shared.Status(); !st.Ready {
		t.Fatalf("runner not ready: %+v", st)
	}
	return shared
}

func runJob(t *testing.T, name, lang, code string, timeout time.Duration, files ...File) Result {
	t.Helper()
	r := itRunner(t)
	job := Job{ID: NewID(), Language: lang, Code: []byte(code), Files: files, Timeout: timeout}
	res, err := r.Run(context.Background(), job)
	if err != nil {
		t.Fatalf("%s: Run: %v", name, err)
	}
	var outs []string
	for _, f := range res.Files {
		outs = append(outs, fmt.Sprintf("%s (%d B)", f.Name, len(f.Data)))
	}
	t.Logf("JOB %-12s exit=%d timedOut=%v oom=%v duration=%dms peak=%.1fMB files=[%s]",
		name, res.ExitCode, res.TimedOut, res.OOMKilled, res.Duration.Milliseconds(),
		float64(res.PeakMemoryBytes)/(1<<20), strings.Join(outs, ", "))
	if t.Failed() || testing.Verbose() {
		if s := strings.TrimSpace(string(res.Stdout.Data)); s != "" {
			t.Logf("  stdout: %s", clip(s))
		}
		if s := strings.TrimSpace(string(res.Stderr.Data)); s != "" {
			t.Logf("  stderr: %s", clip(s))
		}
	}
	assertClean(t, job.ID)
	return res
}

func clip(s string) string {
	if len(s) > 600 {
		return s[:300] + " ... " + s[len(s)-300:]
	}
	return s
}

// assertClean checks the run left no cgroup, run dir or runsc container.
func assertClean(t *testing.T, id string) {
	t.Helper()
	for _, p := range []string{
		filepath.Join(*itCgroup, "kandang", id),
		filepath.Join(*itState, "runs", id),
		filepath.Join(*itState, "runsc", id),
	} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("leftover after the run: %s", p)
		}
	}
}

func TestITLightPython(t *testing.T) {
	res := runJob(t, "light", LangPython, `
import json, math, os, sys
print(json.dumps({"sqrt2": round(math.sqrt(2), 6), "cwd": os.getcwd(), "uid": os.getuid(), "gid": os.getgid()}))
print("to stderr", file=sys.stderr)
`, 30*time.Second)
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s", res.ExitCode, res.Stderr.Data)
	}
	want := `{"sqrt2": 1.414214, "cwd": "/work", "uid": 10001, "gid": 10001}`
	if got := strings.TrimSpace(string(res.Stdout.Data)); got != want {
		t.Errorf("stdout = %s, want %s", got, want)
	}
	if !strings.Contains(string(res.Stderr.Data), "to stderr") {
		t.Errorf("stderr = %q", res.Stderr.Data)
	}
}

func TestITPandasChart(t *testing.T) {
	res := runJob(t, "chart", LangPython, `
import numpy as np, pandas as pd
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
rng = np.random.default_rng(7)
df = pd.DataFrame({"day": np.arange(500_000) % 365, "value": rng.normal(100, 15, 500_000)})
daily = df.groupby("day")["value"].mean()
fig, ax = plt.subplots(figsize=(8, 4))
daily.plot(ax=ax, title="Daily mean")
fig.savefig("/work/out/chart.png", dpi=100)
daily.reset_index().to_excel("/work/out/daily.xlsx", index=False)
print("rows", len(df), "days", len(daily))
`, 120*time.Second)
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s", res.ExitCode, res.Stderr.Data)
	}
	got := map[string][]byte{}
	for _, f := range res.Files {
		got[f.Name] = f.Data
	}
	if png := got["chart.png"]; !bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n")) {
		t.Errorf("chart.png missing or not a PNG (%d bytes)", len(png))
	}
	if xlsx := got["daily.xlsx"]; !bytes.HasPrefix(xlsx, []byte("PK\x03\x04")) {
		t.Errorf("daily.xlsx missing or not a zip (%d bytes)", len(xlsx))
	}
	if !strings.Contains(string(res.Stdout.Data), "rows 500000 days 365") {
		t.Errorf("stdout = %q", res.Stdout.Data)
	}
	if strings.Contains(string(res.Stderr.Data), "temporary cache directory") {
		t.Errorf("matplotlib didn't use the baked cache at /opt/mpl: %s", res.Stderr.Data)
	}
}

func TestITBash(t *testing.T) {
	res := runJob(t, "bash", LangBash, `
set -u
echo "uid=$(id -u) gid=$(id -g) host=$(hostname) cwd=$PWD home=$HOME"
uname -r
touch /usr/kandang-was-here 2>/dev/null && echo "rootfs writable" || echo "rootfs read-only"
touch /tmp/ok && echo "tmp writable"
ls /work
exit 3
`, 30*time.Second)
	out := string(res.Stdout.Data)
	if res.ExitCode != 3 {
		t.Errorf("exit = %d, want 3 (the script's own code)", res.ExitCode)
	}
	for _, want := range []string{"uid=10001 gid=10001 host=kandang cwd=/work home=/tmp", "gvisor", "rootfs read-only", "tmp writable", "main.sh"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout is missing %q:\n%s", want, out)
		}
	}
}

func TestITMemoryLimit(t *testing.T) {
	res := runJob(t, "mem-2g", LangPython, `
b = bytearray(2 * 1024**3)
for i in range(0, len(b), 4096):
    b[i] = 1
print("allocated 2 GB")
`, 60*time.Second)
	if res.ExitCode != ExitOOMKilled {
		t.Errorf("exit = %d, want %d", res.ExitCode, ExitOOMKilled)
	}
	if strings.Contains(string(res.Stdout.Data), "allocated 2 GB") {
		t.Error("the 2 GB allocation succeeded under a 1 GiB limit")
	}
	if res.PeakMemoryBytes > DefaultLimits().MemoryBytes {
		t.Errorf("peak %d is over the limit", res.PeakMemoryBytes)
	}
	if !res.OOMKilled || !strings.Contains(string(res.Stderr.Data), "memory limit") {
		t.Errorf("oom=%v, stderr note missing: %q", res.OOMKilled, res.Stderr.Data)
	}
}

func TestITForkLoop(t *testing.T) {
	res := runJob(t, "fork-loop", LangPython, `
import subprocess
ps, err = [], None
for i in range(1000):
    try:
        ps.append(subprocess.Popen(["sleep", "30"]))
    except OSError as e:
        err = e
        break
print("started", len(ps), "of 1000; error:", err)
for p in ps:
    p.kill()
`, 60*time.Second)
	out := string(res.Stdout.Data)
	if strings.Contains(out, "started 1000 of 1000") {
		t.Fatalf("the process cap didn't hold: %s", out)
	}
	if res.ExitCode != 0 || !strings.Contains(out, "Resource temporarily unavailable") {
		t.Errorf("want a clean EAGAIN at the process limit and exit 0, got exit %d: %s", res.ExitCode, out)
	}
	if res.TimedOut {
		t.Errorf("the run should end on its own once the cap stops it")
	}

	bomb := runJob(t, "fork-bomb", LangBash, `bomb() { bomb | bomb & }; bomb; sleep 2; echo survived`, 10*time.Second)
	if bomb.Duration > 15*time.Second {
		t.Errorf("fork bomb ran %s, past its timeout", bomb.Duration)
	}
}

func TestITTimeout(t *testing.T) {
	res := runJob(t, "busy-3s", LangBash, `while :; do :; done`, 3*time.Second)
	if !res.TimedOut || res.ExitCode != ExitTimedOut {
		t.Errorf("timedOut=%v exit=%d, want true and %d", res.TimedOut, res.ExitCode, ExitTimedOut)
	}
	if res.Duration < 3*time.Second || res.Duration > 4500*time.Millisecond {
		t.Errorf("killed after %s, want about 3 s", res.Duration)
	}
}

func TestITNoNetwork(t *testing.T) {
	res := runJob(t, "socket", LangPython, `
import socket
for target in [("1.1.1.1", 80), ("10.0.0.1", 80), ("8.8.8.8", 53)]:
    try:
        socket.create_connection(target, 3)
        print("CONNECTED", target)
    except OSError as e:
        print("failed", target, e)
try:
    socket.getaddrinfo("example.com", 443)
    print("DNS RESOLVED")
except OSError as e:
    print("dns failed", e)
`, 30*time.Second)
	out := string(res.Stdout.Data)
	if strings.Contains(out, "CONNECTED") || strings.Contains(out, "DNS RESOLVED") {
		t.Errorf("the sandbox reached the network:\n%s", out)
	}
	if strings.Count(out, "failed") != 4 {
		t.Errorf("want 4 failures:\n%s", out)
	}
}

func TestITFilesRoundTrip(t *testing.T) {
	blob := make([]byte, 3<<20)
	rand.Read(blob)
	sum := sha256.Sum256(blob)
	res := runJob(t, "files", LangPython, `
import hashlib, os, shutil
names = sorted(os.listdir("/work/in"))
print("in:", names)
data = open("/work/in/blob.bin", "rb").read()
print("sha256", hashlib.sha256(data).hexdigest())
shutil.copy("/work/in/blob.bin", "/work/out/blob-copy.bin")
with open("/work/in/notes.txt") as f:
    text = f.read()
open("/work/out/NOTES.txt", "w").write(text.upper())
os.mkdir("/work/out/nested")
open("/work/out/nested/skip.txt", "w").write("x")
os.symlink("/etc/passwd", "/work/out/passwd-link")
`, 60*time.Second,
		File{Name: "blob.bin", Data: blob},
		File{Name: "notes.txt", Data: []byte("halo dari kandang\n")},
	)
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %s", res.ExitCode, res.Stderr.Data)
	}
	if !strings.Contains(string(res.Stdout.Data), "sha256 "+hex.EncodeToString(sum[:])) {
		t.Errorf("input didn't arrive intact: %s", res.Stdout.Data)
	}
	got := map[string][]byte{}
	for _, f := range res.Files {
		got[f.Name] = f.Data
	}
	if !bytes.Equal(got["blob-copy.bin"], blob) {
		t.Errorf("blob-copy.bin didn't come back intact (%d bytes)", len(got["blob-copy.bin"]))
	}
	if string(got["NOTES.txt"]) != "HALO DARI KANDANG\n" {
		t.Errorf("NOTES.txt = %q", got["NOTES.txt"])
	}
	if len(res.Files) != 2 || len(res.Dropped) != 2 {
		t.Errorf("files=%d dropped=%v; want 2 files, nested/ and the symlink dropped", len(res.Files), res.Dropped)
	}
}

func TestITTwoAtOnce(t *testing.T) {
	var wg sync.WaitGroup
	start := time.Now()
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := runJob(t, fmt.Sprintf("parallel-%d", i), LangBash, `sleep 2; echo done`, 30*time.Second)
			if res.ExitCode != 0 {
				t.Errorf("parallel %d: exit %d", i, res.ExitCode)
			}
		}()
	}
	wg.Wait()
	if d := time.Since(start); d > 3500*time.Millisecond {
		t.Errorf("two 2 s runs took %s; they should overlap", d)
	}
}

func TestITSweepsLeftovers(t *testing.T) {
	itRunner(t)
	// Fake a crash: a run cgroup and a run dir with nobody behind them.
	id := NewID()
	cg := filepath.Join(*itCgroup, "kandang", id)
	run := filepath.Join(*itState, "runs", id)
	if err := os.Mkdir(cg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(run, "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{
		Runsc: *itRunsc, Rootfs: *itRootfs, StateDir: *itState, CgroupRoot: *itCgroup,
		Limits: DefaultLimits(), Log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}); err != nil {
		t.Fatal(err)
	}
	assertClean(t, id)
}
