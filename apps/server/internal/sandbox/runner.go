package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config is what the real runner needs.
type Config struct {
	Runsc      string // runsc binary, a name on PATH or a path
	Rootfs     string // unpacked image, read-only
	StateDir   string // holds runsc's state (runsc/) and per-run dirs (runs/)
	CgroupRoot string // cgroup v2 mount, usually /sys/fs/cgroup
	Limits     Limits
	Log        *slog.Logger
}

// ErrNotReady is returned when runsc or the rootfs is missing.
var ErrNotReady = errors.New("sandbox: not ready")

// RunscRoot is runsc's --root state directory.
func (c Config) RunscRoot() string { return filepath.Join(c.StateDir, "runsc") }

// RunsDir holds one directory per run: bundle/ and work/.
func (c Config) RunsDir() string { return filepath.Join(c.StateDir, "runs") }

// rootfsMarkers are files the image must have for both languages to work.
var rootfsMarkers = []string{"usr/bin/python3", "bin/bash", "opt/mpl"}

// CheckRootfs reports "ok" or why the rootfs can't be used. It only reads.
func CheckRootfs(path string) (string, bool) {
	if !filepath.IsAbs(path) {
		return "missing: path isn't absolute", false
	}
	for _, m := range rootfsMarkers {
		if _, err := os.Stat(filepath.Join(path, m)); err != nil {
			return "missing: no /" + m + " in " + path, false
		}
	}
	return "ok", true
}

// RunscVersion runs `runsc --version` and returns the release, such as
// "release-20261116.0".
func RunscVersion(ctx context.Context, runsc string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, runsc, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("runsc --version: %w", err)
	}
	return ParseRunscVersion(string(out)), nil
}

// ParseRunscVersion takes the first line of `runsc --version`.
func ParseRunscVersion(out string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	line = strings.TrimSpace(line)
	if v, ok := strings.CutPrefix(line, "runsc version "); ok {
		return v
	}
	return line
}
