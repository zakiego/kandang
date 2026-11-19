// Command kandang runs untrusted Python or bash in a throwaway gVisor
// sandbox, files in and files out, behind a small HTTP API.
//
//	kandang serve     start the HTTP server
//	kandang check     verify runsc, cgroup v2 and the rootfs; writes nothing
//	kandang version   print the version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zakiego/kandang/apps/server/internal/api"
	"github.com/zakiego/kandang/apps/server/internal/config"
	"github.com/zakiego/kandang/apps/server/internal/queue"
	"github.com/zakiego/kandang/apps/server/internal/sandbox"
)

// version is stamped at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `usage: kandang <command> [flags]

commands:
  serve     start the HTTP server
  check     verify runsc, cgroup v2 and the rootfs (writes nothing)
  version   print the version

Run "kandang <command> -h" for the flags.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version", "-v":
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	case "serve":
		cfg, ok := parse("serve", args[1:], stderr)
		if !ok {
			return 2
		}
		if err := serve(cfg, stderr); err != nil {
			_, _ = fmt.Fprintln(stderr, "kandang:", err)
			return 1
		}
		return 0
	case "check":
		cfg, ok := parse("check", args[1:], stderr)
		if !ok {
			return 2
		}
		if !check(cfg, stdout) {
			return 1
		}
		return 0
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "kandang: unknown command %q\n\n%s", args[0], usage)
	return 2
}

func parse(name string, args []string, stderr io.Writer) (config.Config, bool) {
	cfg, err := config.Parse("kandang "+name, args, os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		return cfg, false
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "kandang:", err)
		return cfg, false
	}
	return cfg, true
}

// check verifies what a run needs and prints one line per item. It only
// reads: no cgroup, state dir or file is created.
func check(cfg config.Config, stdout io.Writer) bool {
	ok := true
	line := func(name string, err error, detail string) {
		if err != nil {
			ok = false
			_, _ = fmt.Fprintf(stdout, "FAIL  %-8s %v\n", name, err)
			return
		}
		_, _ = fmt.Fprintf(stdout, "ok    %-8s %s\n", name, detail)
	}
	ver, err := sandbox.RunscVersion(context.Background(), cfg.Runsc)
	line("runsc", err, ver)
	line("cgroup", sandbox.Cgroups{Root: cfg.CgroupRoot}.Check(), cfg.CgroupRoot+" has memory, pids, cpu")
	if msg, good := sandbox.CheckRootfs(cfg.Rootfs); good {
		line("rootfs", nil, cfg.Rootfs)
	} else {
		line("rootfs", errors.New(msg), "")
	}
	_, err = config.ReadToken(cfg.TokenFile)
	line("token", err, cfg.TokenFile+" (0600)")
	return ok
}

func serve(cfg config.Config, stderr io.Writer) error {
	log := slog.New(slog.NewJSONHandler(stderr, nil))
	token, err := config.ReadToken(cfg.TokenFile)
	if err != nil {
		return err
	}
	runner, err := sandbox.New(sandbox.Config{
		Runsc: cfg.Runsc, Rootfs: cfg.Rootfs, StateDir: cfg.StateDir,
		CgroupRoot: cfg.CgroupRoot, Limits: cfg.Limits, Log: log,
	})
	if err != nil {
		return err
	}
	if st := runner.Status(); !st.Ready {
		// Keep serving: /health says what's missing and runs get 503 until
		// a deploy fixes it.
		log.Warn("sandbox not ready", "runsc", st.Runsc, "rootfs", st.Rootfs)
	}
	handler := api.New(api.Options{
		Runner: runner, Queue: queue.New(cfg.MaxRunning, cfg.MaxWaiting), Token: token,
		Version: version, Limits: cfg.Limits, Log: log, MaxLargeBodies: cfg.MaxRunning,
	})
	// Every request context derives from base, so a shutdown cancels the runs
	// in flight, which kills their sandboxes and runs their cleanup.
	base, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return base },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	log.Info("kandang listening", "addr", ln.Addr().String(), "version", version,
		"maxRunning", cfg.MaxRunning, "maxWaiting", cfg.MaxWaiting)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	cancelBase()
	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	return nil
}
