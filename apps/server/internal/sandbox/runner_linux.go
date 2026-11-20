//go:build linux

package sandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// runscRunner drives runsc directly: no Docker, one OCI bundle and one
// cgroup per run.
type runscRunner struct {
	cfg Config
	cg  Cgroups
	log *slog.Logger

	mu       sync.Mutex
	runscVer string // cached once runsc answered
}

// New checks the config, prepares the state dirs and the cgroup parent, and
// sweeps whatever an earlier crash left behind.
func New(cfg Config) (Runner, error) {
	if err := cfg.Limits.Validate(); err != nil {
		return nil, err
	}
	for _, p := range []string{cfg.Rootfs, cfg.StateDir, cfg.CgroupRoot} {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("sandbox: %q must be an absolute path", p)
		}
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	r := &runscRunner{cfg: cfg, cg: Cgroups{Root: cfg.CgroupRoot}, log: cfg.Log}
	for _, d := range []string{cfg.StateDir, cfg.RunscRoot(), cfg.RunsDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	if err := r.cg.Setup(); err != nil {
		return nil, err
	}
	r.sweep()
	return r, nil
}

func (r *runscRunner) Status() Status {
	rootfs, rootOK := CheckRootfs(r.cfg.Rootfs)
	r.mu.Lock()
	ver := r.runscVer
	r.mu.Unlock()
	runscOK := ver != ""
	if !runscOK {
		v, err := RunscVersion(context.Background(), r.cfg.Runsc)
		if err != nil {
			ver = "missing: " + err.Error()
		} else {
			ver, runscOK = v, true
			r.mu.Lock()
			r.runscVer = v
			r.mu.Unlock()
		}
	}
	return Status{Ready: rootOK && runscOK, Runsc: ver, Rootfs: rootfs}
}

func (r *runscRunner) runsc(ctx context.Context, args ...string) *exec.Cmd {
	base := []string{"--root", r.cfg.RunscRoot(), "--ignore-cgroups", "--network=none", "--platform=systrap"}
	cmd := exec.CommandContext(ctx, r.cfg.Runsc, append(base, args...)...)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	return cmd
}

// runscQuiet runs a housekeeping command (kill, delete) with a short deadline
// and ignores its result: the cgroup kill is what actually stops a run.
func (r *runscRunner) runscQuiet(args ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = r.runsc(ctx, args...).Run()
}

func (r *runscRunner) Run(ctx context.Context, job Job) (res Result, err error) {
	if !ValidID(job.ID) {
		return Result{}, ErrBadID
	}
	if !r.Status().Ready {
		return Result{}, ErrNotReady
	}
	if job.Timeout <= 0 || job.Timeout > r.cfg.Limits.MaxTimeout {
		return Result{}, fmt.Errorf("sandbox: timeout %s out of range", job.Timeout)
	}
	runDir := filepath.Join(r.cfg.RunsDir(), job.ID)
	bundleDir := filepath.Join(runDir, "bundle")
	// One size-capped host tmpfs per run holds everything the sandbox may
	// write to on the host side: work/ (bind-mounted at /work) and mpl/ (a
	// writable copy of the image's matplotlib cache at /opt/mpl).
	fsDir := filepath.Join(runDir, "fs")
	workDir := filepath.Join(fsDir, "work")
	mplDir := filepath.Join(fsDir, "mpl")

	var cg *Cgroup
	if err := os.Mkdir(runDir, 0o700); err != nil {
		return Result{}, err
	}
	defer func() { r.cleanup(job.ID, runDir, cg) }()

	for _, d := range []string{bundleDir, fsDir} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return Result{}, err
		}
	}
	opts := fmt.Sprintf("size=%d,mode=0755", r.cfg.Limits.WorkBytes)
	if err := syscall.Mount("tmpfs", fsDir, "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, opts); err != nil {
		return Result{}, fmt.Errorf("sandbox: mount the run tmpfs: %w", err)
	}
	chown := func(p string) error { return os.Lchown(p, SandboxUID, SandboxUID) }
	for _, d := range []string{workDir, mplDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			return Result{}, err
		}
		if err := chown(d); err != nil {
			return Result{}, err
		}
	}
	if err := StageWork(workDir, job, chown); err != nil {
		return Result{}, fmt.Errorf("sandbox: stage inputs: %w", err)
	}
	if err := SeedDir(filepath.Join(r.cfg.Rootfs, "opt", "mpl"), mplDir, chown); err != nil {
		return Result{}, fmt.Errorf("sandbox: seed the matplotlib cache: %w", err)
	}
	if err := WriteBundle(bundleDir, BundleSpec{
		Rootfs: r.cfg.Rootfs, WorkHost: workDir, MplHost: mplDir, Language: job.Language, Limits: r.cfg.Limits,
	}); err != nil {
		return Result{}, fmt.Errorf("sandbox: bundle: %w", err)
	}
	cg, err = r.cg.Create(job.ID, r.cfg.Limits)
	if err != nil {
		return Result{}, err
	}
	cgDir, err := os.Open(cg.Path)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = cgDir.Close() }()

	stdout, stderr := NewCapWriter(MaxStreamBytes), NewCapWriter(MaxStreamBytes)
	// note appends one of kandang's own lines to the run's stderr. A CapWriter
	// never fails, so there is no error to pass on.
	note := func(format string, a ...any) { _, _ = fmt.Fprintf(stderr, format, a...) }
	// Not CommandContext: a timeout or a cancel kills the whole cgroup, not
	// just the runsc process.
	cmd := r.runsc(context.Background(), "run", "--bundle", bundleDir, job.ID)
	cmd.Dir = bundleDir
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 5 * time.Second
	// runsc starts inside the run's cgroup (clone3 with CLONE_INTO_CGROUP),
	// so everything it spawns (sentry, gofer) is limited from the first
	// instruction.
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(cgDir.Fd()), Setpgid: true}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("sandbox: start runsc: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(job.Timeout)
	defer timer.Stop()
	var waitErr error
	timedOut, canceled := false, false
	select {
	case waitErr = <-done:
	case <-timer.C:
		timedOut = true
		waitErr, err = r.killAndReap(job.ID, cg, cmd, done)
	case <-ctx.Done():
		canceled = true
		waitErr, err = r.killAndReap(job.ID, cg, cmd, done)
	}
	res.Duration = time.Since(start)
	if err != nil {
		return Result{}, err
	}
	if canceled {
		return Result{}, ctx.Err()
	}

	res.ExitCode, err = exitCode(cmd, waitErr)
	if err != nil {
		return Result{}, err
	}
	res.PeakMemoryBytes = cg.Peak()
	res.OOMKilled = cg.OOMKills() > 0
	res.TimedOut = timedOut
	if cg.PidsLimited() {
		note("\n[kandang] the run hit its process limit (%d processes); fork or thread creation failed past it\n", r.cfg.Limits.Procs)
	}
	switch {
	case timedOut:
		res.ExitCode = ExitTimedOut
		note("\n[kandang] stopped: the run hit its %s timeout\n", job.Timeout)
	case res.OOMKilled:
		res.ExitCode = ExitOOMKilled
		note("\n[kandang] stopped: the run went over its %d MiB memory limit\n", r.cfg.Limits.MemoryBytes>>20)
	}
	res.Stdout, res.Stderr = stdout.Output(), stderr.Output()

	// runsc has exited, but that alone doesn't prove the sentry and the gofer
	// are gone (runsc can die first when the cgroup runs out of pids). Nothing
	// from the run may still be alive while its outputs are read.
	if err := cg.KillAndWait(10 * time.Second); err != nil {
		return Result{}, fmt.Errorf("sandbox: %w", err)
	}
	res.Files, res.Dropped, err = CollectOutputs(workDir)
	if err != nil {
		return Result{}, fmt.Errorf("sandbox: collect outputs: %w", err)
	}
	return res, nil
}

func exitCode(cmd *exec.Cmd, waitErr error) (int, error) {
	if waitErr != nil && !errors.Is(waitErr, exec.ErrWaitDelay) {
		var ee *exec.ExitError
		if !errors.As(waitErr, &ee) {
			return 0, fmt.Errorf("sandbox: wait for runsc: %w", waitErr)
		}
	}
	if cmd.ProcessState == nil {
		return 0, fmt.Errorf("sandbox: runsc has no exit state")
	}
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if ok && ws.Signaled() {
		return 128 + int(ws.Signal()), nil
	}
	return cmd.ProcessState.ExitCode(), nil
}

// kill stops a run: SIGKILL to the whole cgroup first, then runsc's own kill
// so its state matches.
func (r *runscRunner) kill(id string, cg *Cgroup) {
	if err := cg.Kill(); err != nil {
		r.log.Warn("cgroup kill failed", "run", id, "err", err)
	}
	r.runscQuiet("kill", id, "KILL")
}

// killAndReap stops a run and waits for runsc to be reaped. The cgroup kill
// normally takes runsc down at once. If it doesn't (the write to cgroup.kill
// failed, say), waiting forever would hold a run slot for good, so runsc's
// process group gets a SIGKILL of its own, and after that the run is given
// up as an error.
func (r *runscRunner) killAndReap(id string, cg *Cgroup, cmd *exec.Cmd, done <-chan error) (error, error) {
	r.kill(id, cg)
	select {
	case waitErr := <-done:
		return waitErr, nil
	case <-time.After(10 * time.Second):
	}
	r.log.Warn("runsc survived the cgroup kill; killing its process group", "run", id)
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	select {
	case waitErr := <-done:
		return waitErr, nil
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("sandbox: runsc for run %s didn't exit after it was killed", id)
	}
}

// cleanup always runs after a run, whatever happened: delete the container,
// kill and remove the cgroup, unmount the run tmpfs, remove the run dir.
func (r *runscRunner) cleanup(id, runDir string, cg *Cgroup) {
	r.runscQuiet("delete", "--force", id)
	if cg != nil {
		if err := cg.Remove(5 * time.Second); err != nil {
			r.log.Warn("cgroup remove failed", "run", id, "err", err)
		}
	}
	fsDir := filepath.Join(runDir, "fs")
	if err := syscall.Unmount(fsDir, syscall.MNT_DETACH); err != nil && !errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOENT) {
		r.log.Warn("unmount the run tmpfs failed", "run", id, "err", err)
	}
	if err := os.RemoveAll(runDir); err != nil {
		r.log.Warn("remove run dir failed", "run", id, "err", err)
	}
}

// sweep removes what a crash left behind: run cgroups, runsc containers and
// run dirs (with their /work mounts).
func (r *runscRunner) sweep() {
	cgs, err := r.cg.Leftovers()
	if err != nil {
		r.log.Warn("sweep: list cgroups", "err", err)
	}
	for _, g := range cgs {
		if err := g.Remove(5 * time.Second); err != nil {
			r.log.Warn("sweep: cgroup", "path", g.Path, "err", err)
		}
	}
	ids := map[string]bool{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	out, err := r.runsc(ctx, "list", "--quiet").Output()
	cancel()
	if err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(out)))
		for sc.Scan() {
			if id := strings.TrimSpace(sc.Text()); ValidID(id) {
				ids[id] = true
			}
		}
	}
	entries, _ := os.ReadDir(r.cfg.RunsDir())
	for _, e := range entries {
		if ValidID(e.Name()) {
			ids[e.Name()] = true
		}
	}
	for id := range ids {
		r.cleanup(id, filepath.Join(r.cfg.RunsDir(), id), nil)
	}
	if n := len(cgs) + len(ids); n > 0 {
		r.log.Info("swept leftovers from an earlier run", "cgroups", len(cgs), "runs", len(ids))
	}
}
