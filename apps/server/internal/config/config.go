// Package config reads kandang's settings from flags, then the environment,
// then the defaults, in that order of precedence.
package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zakiego/kandang/apps/server/internal/sandbox"
)

// Config is everything `kandang serve` and `kandang check` need.
type Config struct {
	Listen     string
	TokenFile  string
	Runsc      string
	Rootfs     string
	StateDir   string
	CgroupRoot string
	MaxRunning int
	MaxWaiting int
	Limits     sandbox.Limits
}

// Defaults match the layout deploy/machine-setup.sh creates.
func Defaults() Config {
	return Config{
		Listen:     ":8080",
		TokenFile:  "/etc/kandang/token",
		Runsc:      "runsc",
		Rootfs:     "/var/lib/kandang/rootfs",
		StateDir:   "/var/lib/kandang/state",
		CgroupRoot: "/sys/fs/cgroup",
		MaxRunning: 2,
		MaxWaiting: 10,
		Limits:     sandbox.DefaultLimits(),
	}
}

// Env names, one per setting. Flags with the same meaning win over these.
const (
	EnvListen     = "KANDANG_LISTEN"
	EnvTokenFile  = "KANDANG_TOKEN_FILE"
	EnvRunsc      = "KANDANG_RUNSC"
	EnvRootfs     = "KANDANG_ROOTFS"
	EnvStateDir   = "KANDANG_STATE_DIR"
	EnvCgroupRoot = "KANDANG_CGROUP_ROOT"
	EnvMaxRunning = "KANDANG_MAX_RUNNING"
	EnvMaxWaiting = "KANDANG_MAX_WAITING"
	EnvMemoryMB   = "KANDANG_MEMORY_MB"
	EnvPids       = "KANDANG_PIDS"
	EnvProcs      = "KANDANG_PROCS"
	EnvTimeout    = "KANDANG_DEFAULT_TIMEOUT_SEC"
	EnvMaxTimeout = "KANDANG_MAX_TIMEOUT_SEC"
)

// Parse reads args (without the program and subcommand names) and the
// environment through getenv. It returns flag.ErrHelp for -h.
func Parse(name string, args []string, getenv func(string) string) (Config, error) {
	c := Defaults()
	var errs []error
	str := func(dst *string, env string) {
		if v := strings.TrimSpace(getenv(env)); v != "" {
			*dst = v
		}
	}
	num := func(dst *int, env string) {
		if v := strings.TrimSpace(getenv(env)); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %q isn't a number", env, v))
				return
			}
			*dst = n
		}
	}
	str(&c.Listen, EnvListen)
	str(&c.TokenFile, EnvTokenFile)
	str(&c.Runsc, EnvRunsc)
	str(&c.Rootfs, EnvRootfs)
	str(&c.StateDir, EnvStateDir)
	str(&c.CgroupRoot, EnvCgroupRoot)
	num(&c.MaxRunning, EnvMaxRunning)
	num(&c.MaxWaiting, EnvMaxWaiting)
	memMB := int(c.Limits.MemoryBytes >> 20)
	num(&memMB, EnvMemoryMB)
	num(&c.Limits.Pids, EnvPids)
	num(&c.Limits.Procs, EnvProcs)
	timeout := int(c.Limits.DefaultTimeout / time.Second)
	maxTimeout := int(c.Limits.MaxTimeout / time.Second)
	num(&timeout, EnvTimeout)
	num(&maxTimeout, EnvMaxTimeout)
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&c.Listen, "listen", c.Listen, "address to listen on ("+EnvListen+")")
	fs.StringVar(&c.TokenFile, "token-file", c.TokenFile, "file holding the bearer token ("+EnvTokenFile+")")
	fs.StringVar(&c.Runsc, "runsc", c.Runsc, "runsc binary ("+EnvRunsc+")")
	fs.StringVar(&c.Rootfs, "rootfs", c.Rootfs, "unpacked sandbox image, read-only ("+EnvRootfs+")")
	fs.StringVar(&c.StateDir, "state-dir", c.StateDir, "runsc state and per-run dirs ("+EnvStateDir+")")
	fs.StringVar(&c.CgroupRoot, "cgroup-root", c.CgroupRoot, "cgroup v2 mount ("+EnvCgroupRoot+")")
	fs.IntVar(&c.MaxRunning, "max-running", c.MaxRunning, "runs at once ("+EnvMaxRunning+")")
	fs.IntVar(&c.MaxWaiting, "max-waiting", c.MaxWaiting, "runs waiting for a slot before 429 ("+EnvMaxWaiting+")")
	fs.IntVar(&memMB, "memory-mb", memMB, "memory per run in MiB, no swap ("+EnvMemoryMB+")")
	fs.IntVar(&c.Limits.Pids, "pids", c.Limits.Pids, "pids.max per run, gVisor threads included ("+EnvPids+")")
	fs.IntVar(&c.Limits.Procs, "procs", c.Limits.Procs, "processes per run inside the sandbox, RLIMIT_NPROC ("+EnvProcs+")")
	fs.IntVar(&timeout, "default-timeout-sec", timeout, "timeout when a run doesn't set one ("+EnvTimeout+")")
	fs.IntVar(&maxTimeout, "max-timeout-sec", maxTimeout, "largest timeout a run may ask for ("+EnvMaxTimeout+")")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	c.Limits.MemoryBytes = int64(memMB) << 20
	c.Limits.DefaultTimeout = time.Duration(timeout) * time.Second
	c.Limits.MaxTimeout = time.Duration(maxTimeout) * time.Second
	return c, c.Validate()
}

// Validate checks the settings that don't need the filesystem.
func (c Config) Validate() error {
	if c.MaxRunning < 1 {
		return fmt.Errorf("max-running must be at least 1")
	}
	if c.MaxWaiting < 0 {
		return fmt.Errorf("max-waiting can't be negative")
	}
	for name, p := range map[string]string{"rootfs": c.Rootfs, "state-dir": c.StateDir, "cgroup-root": c.CgroupRoot, "token-file": c.TokenFile} {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("%s must be an absolute path, got %q", name, p)
		}
	}
	return c.Limits.Validate()
}

// MinTokenLen is the shortest token kandang accepts, so a typo or an empty
// file can't open the API.
const MinTokenLen = 32

// ReadToken reads the bearer token. The file must not be readable by group
// or others.
func ReadToken(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("token file: %w", err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("token file %s is mode %o; it must be 0600 or stricter", path, st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("token file: %w", err)
	}
	tok := []byte(strings.TrimSpace(string(b)))
	if len(tok) < MinTokenLen {
		return nil, fmt.Errorf("token in %s is shorter than %d characters", path, MinTokenLen)
	}
	return tok, nil
}
