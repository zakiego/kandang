//go:build !linux

package sandbox

import (
	"context"
	"runtime"
)

// stubRunner stands in on platforms without runsc, so the server builds and
// its tests run on macOS. It never runs anything.
type stubRunner struct{ cfg Config }

// New returns a runner that reports itself as not ready.
func New(cfg Config) (Runner, error) {
	return stubRunner{cfg: cfg}, nil
}

func (stubRunner) Run(context.Context, Job) (Result, error) {
	return Result{}, ErrUnsupported
}

func (s stubRunner) Status() Status {
	rootfs, _ := CheckRootfs(s.cfg.Rootfs)
	return Status{Ready: false, Runsc: "unsupported on " + runtime.GOOS, Rootfs: rootfs}
}
