package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// The OCI runtime spec, cut down to the fields kandang sets. runsc reads the
// bundle's config.json; anything not listed here stays at runsc's default.

type ociSpec struct {
	OCIVersion string     `json:"ociVersion"`
	Process    ociProcess `json:"process"`
	Root       ociRoot    `json:"root"`
	Hostname   string     `json:"hostname"`
	Mounts     []ociMount `json:"mounts"`
	Linux      ociLinux   `json:"linux"`
}

type ociProcess struct {
	Terminal        bool            `json:"terminal"`
	User            ociUser         `json:"user"`
	Args            []string        `json:"args"`
	Env             []string        `json:"env"`
	Cwd             string          `json:"cwd"`
	Capabilities    ociCapabilities `json:"capabilities"`
	Rlimits         []ociRlimit     `json:"rlimits"`
	NoNewPrivileges bool            `json:"noNewPrivileges"`
}

type ociUser struct {
	UID uint32 `json:"uid"`
	GID uint32 `json:"gid"`
}

// Empty lists, not null: an empty set is what drops every capability.
type ociCapabilities struct {
	Bounding    []string `json:"bounding"`
	Effective   []string `json:"effective"`
	Inheritable []string `json:"inheritable"`
	Permitted   []string `json:"permitted"`
	Ambient     []string `json:"ambient"`
}

type ociRlimit struct {
	Type string `json:"type"`
	Hard uint64 `json:"hard"`
	Soft uint64 `json:"soft"`
}

type ociRoot struct {
	Path     string `json:"path"`
	Readonly bool   `json:"readonly"`
}

type ociMount struct {
	Destination string   `json:"destination"`
	Type        string   `json:"type"`
	Source      string   `json:"source"`
	Options     []string `json:"options,omitempty"`
}

type ociLinux struct {
	Namespaces []ociNamespace `json:"namespaces"`
}

type ociNamespace struct {
	Type string `json:"type"`
}

// Paths inside the sandbox.
const (
	WorkDir = "/work"
	InDir   = "/work/in"
	OutDir  = "/work/out"
)

// Env is the whole environment a run gets.
var Env = []string{
	"PATH=/usr/local/bin:/usr/bin:/bin",
	"HOME=/tmp",
	"PYTHONDONTWRITEBYTECODE=1",
	"PYTHONUNBUFFERED=1",
	"MPLCONFIGDIR=/opt/mpl",
}

// ScriptName is the file the code is written to inside /work.
func ScriptName(language string) (string, error) {
	switch language {
	case LangPython:
		return "main.py", nil
	case LangBash:
		return "main.sh", nil
	}
	return "", fmt.Errorf("unknown language %q", language)
}

// Command is the argv that runs the script.
func Command(language string) ([]string, error) {
	name, err := ScriptName(language)
	if err != nil {
		return nil, err
	}
	interp := "python3"
	if language == LangBash {
		interp = "bash"
	}
	return []string{interp, WorkDir + "/" + name}, nil
}

// BundleSpec is what a run's config.json is built from.
type BundleSpec struct {
	Rootfs   string // read-only rootfs on the host, absolute
	WorkHost string // per-run host tmpfs dir, bind-mounted at /work
	MplHost  string // optional writable copy of /opt/mpl from the same tmpfs
	Language string
	Limits   Limits
}

// Bundle returns the run's config.json. The rootfs is shared and read-only;
// /tmp is a tmpfs inside the sandbox; /work is a dir on the per-run host
// tmpfs that holds the script, /work/in and /work/out, so kandang can stage
// inputs before and read outputs after. The process has no capabilities, can't gain
// privileges, and gets its own pid, network, ipc, uts and mount namespaces.
func Bundle(s BundleSpec) ([]byte, error) {
	if !filepath.IsAbs(s.Rootfs) || !filepath.IsAbs(s.WorkHost) || (s.MplHost != "" && !filepath.IsAbs(s.MplHost)) {
		return nil, fmt.Errorf("bundle paths must be absolute")
	}
	args, err := Command(s.Language)
	if err != nil {
		return nil, err
	}
	none := []string{}
	spec := ociSpec{
		OCIVersion: "1.0.2",
		Process: ociProcess{
			User: ociUser{UID: SandboxUID, GID: SandboxUID},
			Args: args,
			Env:  append([]string(nil), Env...),
			Cwd:  WorkDir,
			Capabilities: ociCapabilities{
				Bounding: none, Effective: none, Inheritable: none, Permitted: none, Ambient: none,
			},
			Rlimits: []ociRlimit{
				{Type: "RLIMIT_NOFILE", Hard: NoFileLimit, Soft: NoFileLimit},
				// gVisor enforces this itself and returns EAGAIN, well before
				// pids.max starves the sentry of host threads (which kills the
				// whole sandbox with no message).
				{Type: "RLIMIT_NPROC", Hard: uint64(s.Limits.Procs), Soft: uint64(s.Limits.Procs)},
			},
			NoNewPrivileges: true,
		},
		Root:     ociRoot{Path: s.Rootfs, Readonly: true},
		Hostname: "kandang",
		Mounts: []ociMount{
			{Destination: "/proc", Type: "proc", Source: "proc"},
			{Destination: "/dev", Type: "tmpfs", Source: "tmpfs", Options: []string{"nosuid", "mode=755"}},
			{Destination: "/tmp", Type: "tmpfs", Source: "tmpfs", Options: []string{
				"nosuid", "nodev", fmt.Sprintf("size=%d", s.Limits.TmpBytes), "mode=1777",
			}},
			{Destination: WorkDir, Type: "bind", Source: s.WorkHost, Options: []string{"rbind", "rw", "nosuid", "nodev"}},
		},
		Linux: ociLinux{Namespaces: []ociNamespace{
			{Type: "pid"}, {Type: "network"}, {Type: "ipc"}, {Type: "uts"}, {Type: "mount"},
		}},
	}
	if s.MplHost != "" {
		// matplotlib only uses MPLCONFIGDIR when it can write there, and the
		// rootfs is read-only, so each run gets a writable copy of the
		// image's font cache instead of rebuilding it in /tmp.
		spec.Mounts = append(spec.Mounts, ociMount{
			Destination: "/opt/mpl", Type: "bind", Source: s.MplHost, Options: []string{"rbind", "rw", "nosuid", "nodev"},
		})
	}
	return json.MarshalIndent(spec, "", "  ")
}

// WriteBundle writes config.json into dir.
func WriteBundle(dir string, s BundleSpec) error {
	b, err := Bundle(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600)
}
