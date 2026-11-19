// Package sandbox runs one job in a fresh gVisor sandbox: it stages the code
// and the input files, writes an OCI bundle, puts runsc in its own cgroup,
// enforces the timeout, collects the outputs and removes everything after.
//
// The pieces that only shape data (names, limits, the bundle, cgroup files,
// output collection, truncation) build on every platform so they can be
// tested on macOS. The runner that drives runsc builds on Linux only.
package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"regexp"
	"time"
)

// Languages a job can use.
const (
	LangPython = "python"
	LangBash   = "bash"
)

// Exit codes the runner reports for its own kills, matching coreutils
// timeout(1) and a shell's 128+SIGKILL.
const (
	ExitTimedOut  = 124
	ExitOOMKilled = 137
)

// SandboxUID is the uid and gid the code runs as. The image creates the
// same user, and the staged files are owned by it.
const SandboxUID = 10001

// File is a named blob going into or coming out of a run.
type File struct {
	Name string
	Data []byte
}

// Dropped is an output file that wasn't returned, with the reason.
type Dropped struct {
	Name   string
	Reason string
}

// Job is one validated run request.
type Job struct {
	ID       string
	Language string
	Code     []byte
	Files    []File
	Timeout  time.Duration
}

// Result is what a finished run reports.
type Result struct {
	ExitCode        int
	TimedOut        bool
	OOMKilled       bool
	Duration        time.Duration
	PeakMemoryBytes int64
	Stdout          Output
	Stderr          Output
	Files           []File
	Dropped         []Dropped
}

// Status says whether the runner can take jobs.
type Status struct {
	Ready  bool
	Runsc  string // runsc version, or why it's missing
	Rootfs string // "ok", or why it's missing
}

// Runner runs jobs. The API and the queue only see this interface, so they
// are tested with a fake.
type Runner interface {
	Run(ctx context.Context, job Job) (Result, error)
	Status() Status
}

// ErrUnsupported is returned by the runner on platforms without runsc.
var ErrUnsupported = errors.New("sandbox: runs need Linux with runsc; this build can't run them")

// ErrBadID is returned for a run id that isn't one NewID could have made.
var ErrBadID = errors.New("sandbox: invalid run id")

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)

// NewID returns a random, URL-safe run id (16 random bytes, 22 characters).
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// ValidID reports whether id is safe to use as a path element.
func ValidID(id string) bool {
	return idPattern.MatchString(id)
}
