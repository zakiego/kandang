package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := Parse("serve", nil, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" || c.MaxRunning != 2 || c.MaxWaiting != 10 || c.TokenFile != "/etc/kandang/token" {
		t.Errorf("defaults = %+v", c)
	}
	if c.Limits.MemoryBytes != 1<<30 || c.Limits.Pids != 256 || c.Limits.DefaultTimeout != time.Minute {
		t.Errorf("limits = %+v", c.Limits)
	}
}

func TestEnvThenFlags(t *testing.T) {
	e := env(map[string]string{
		EnvListen:     "127.0.0.1:9000",
		EnvMaxRunning: "3",
		EnvMemoryMB:   "512",
		EnvRootfs:     "/srv/rootfs",
	})
	c, err := Parse("serve", []string{"-listen", ":7000", "-max-waiting", "4"}, e)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":7000" {
		t.Errorf("flag should win: listen = %s", c.Listen)
	}
	if c.MaxRunning != 3 || c.MaxWaiting != 4 || c.Rootfs != "/srv/rootfs" || c.Limits.MemoryBytes != 512<<20 {
		t.Errorf("got %+v", c)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		args []string
		env  map[string]string
		want string
	}{
		{nil, map[string]string{EnvMaxRunning: "two"}, "isn't a number"},
		{[]string{"-max-running", "0"}, nil, "max-running"},
		{[]string{"-rootfs", "rootfs"}, nil, "absolute"},
		{[]string{"-memory-mb", "1"}, nil, "memory"},
		{[]string{"-max-timeout-sec", "10"}, nil, "timeouts"},
		{[]string{"-pids", "100"}, nil, "process limit"},
		{[]string{"extra"}, nil, "unexpected argument"},
	}
	for _, c := range cases {
		_, err := Parse("serve", c.args, env(c.env))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Parse(%v, %v) = %v, want %q", c.args, c.env, err, c.want)
		}
	}
	if _, err := Parse("serve", []string{"-h"}, env(nil)); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("-h = %v", err)
	}
}

func TestReadToken(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "token")
	os.WriteFile(good, []byte(strings.Repeat("k", 43)+"\n"), 0o600)
	tok, err := ReadToken(good)
	if err != nil || string(tok) != strings.Repeat("k", 43) {
		t.Errorf("ReadToken = %q, %v", tok, err)
	}

	open := filepath.Join(dir, "open")
	os.WriteFile(open, []byte(strings.Repeat("k", 43)), 0o644)
	os.Chmod(open, 0o644)
	if _, err := ReadToken(open); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Errorf("world-readable token: %v", err)
	}

	short := filepath.Join(dir, "short")
	os.WriteFile(short, []byte("abc"), 0o600)
	if _, err := ReadToken(short); err == nil {
		t.Error("short token accepted")
	}
	if _, err := ReadToken(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing token file accepted")
	}
}
