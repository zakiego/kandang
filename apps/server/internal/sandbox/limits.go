package sandbox

import (
	"fmt"
	"time"
)

// Fixed request and output limits. They are part of the API contract, so
// they are constants rather than settings.
const (
	MaxCodeBytes      = 100 << 10 // 100 KiB of code
	MaxInputFiles     = 20
	MaxInputFileBytes = 15 << 20 // 15 MiB per input file
	MaxInputBytes     = 50 << 20 // 50 MiB of input files in total
	MaxNameBytes      = 128

	MaxOutputFiles     = 10
	MaxOutputFileBytes = 15 << 20 // 15 MiB per output file
	MaxOutputScan      = 1000     // entries of /work/out kandang looks at
	MaxDroppedListed   = 100      // dropped outputs named in a result

	MaxStreamBytes = 32 << 10 // stdout and stderr, each

	NoFileLimit = 1024
)

// Limits are the per-run resource limits. The zero value isn't useful; start
// from DefaultLimits.
type Limits struct {
	MemoryBytes    int64         // memory.max; swap is always 0
	Pids           int           // pids.max; gVisor's own threads count too
	Procs          int           // RLIMIT_NPROC inside the sandbox, enforced by gVisor
	CPUMax         string        // cpu.max, "quota period"
	TmpBytes       int64         // tmpfs /tmp inside the sandbox
	WorkBytes      int64         // tmpfs /work (host side, bind-mounted)
	DefaultTimeout time.Duration // when a job doesn't say
	MaxTimeout     time.Duration // the most a job may ask for
}

// DefaultLimits are the limits from the spec: 1 GiB, 256 pids, one CPU,
// 256 MiB /tmp, 512 MiB /work, 60 s default and 300 s max timeout. Procs is
// 64: measured with runsc release-20261116.0, 64 processes inside take
// about 156 host pids, which leaves gVisor room under pids.max 256.
func DefaultLimits() Limits {
	return Limits{
		MemoryBytes:    1 << 30,
		Pids:           256,
		Procs:          64,
		CPUMax:         "100000 100000",
		TmpBytes:       256 << 20,
		WorkBytes:      512 << 20,
		DefaultTimeout: 60 * time.Second,
		MaxTimeout:     300 * time.Second,
	}
}

// Validate checks that the limits make sense.
func (l Limits) Validate() error {
	switch {
	case l.MemoryBytes < 64<<20:
		return fmt.Errorf("memory limit %d is below 64 MiB", l.MemoryBytes)
	case l.Pids < 32:
		return fmt.Errorf("pids limit %d is below 32 (gVisor needs its own threads)", l.Pids)
	case l.Procs < 1 || l.Procs*2 > l.Pids:
		return fmt.Errorf("process limit %d must be at least 1 and at most half of pids %d", l.Procs, l.Pids)
	case l.CPUMax == "":
		return fmt.Errorf("cpu.max is empty")
	case l.TmpBytes <= 0 || l.WorkBytes <= 0:
		return fmt.Errorf("tmpfs sizes must be positive")
	case l.WorkBytes < MaxInputBytes+MaxCodeBytes:
		return fmt.Errorf("work size %d can't hold the largest input", l.WorkBytes)
	case l.DefaultTimeout <= 0 || l.MaxTimeout < l.DefaultTimeout:
		return fmt.Errorf("timeouts: default %s, max %s", l.DefaultTimeout, l.MaxTimeout)
	}
	return nil
}
