# Kandang server

Kandang runs untrusted Python or bash for AI agents. Every run gets a fresh gVisor sandbox with no network, files go in and files come out, and the sandbox is gone when the run ends. The HTTP API knows nothing about its clients: it never receives or holds a secret, and the only token it has guards its own API.

It's one Go binary (`kandang`, standard library only, no cgo) that drives `runsc` directly. There is no Docker on the run path and no Docker socket anywhere. It's built to live in an isolated OrbStack machine (no Mac file sharing, no route to other machines or host ports), where Docker can't run, so it writes the OCI bundles and per-run cgroups itself.

## Commands

```
kandang serve     start the HTTP server
kandang check     verify runsc, cgroup v2, the rootfs and the token file (writes nothing)
kandang version   print the version
```

Every setting is a flag or an environment variable; flags win. Run `kandang serve -h` for the list. The ones you may want:

| Flag | Env | Default |
|---|---|---|
| `-listen` | `KANDANG_LISTEN` | `:8080` |
| `-token-file` | `KANDANG_TOKEN_FILE` | `/etc/kandang/token` |
| `-rootfs` | `KANDANG_ROOTFS` | `/var/lib/kandang/rootfs` |
| `-state-dir` | `KANDANG_STATE_DIR` | `/var/lib/kandang/state` |
| `-cgroup-root` | `KANDANG_CGROUP_ROOT` | `/sys/fs/cgroup` |
| `-max-running` | `KANDANG_MAX_RUNNING` | `2` |
| `-max-waiting` | `KANDANG_MAX_WAITING` | `10` |

The systemd unit reads overrides from `/etc/kandang/kandang.conf` (one `KEY=value` per line) if it exists.

## API

### `GET /health`

No auth. 200 when runs can start, 503 when `runsc` or the rootfs is missing.

```json
{"ok": true, "version": "v0.1.0", "runsc": "release-20261116.0", "rootfs": "ok", "queue": {"running": 0, "waiting": 0}}
```

### `POST /v1/runs`

Needs `Authorization: Bearer <token>`. The token is read from the token file at start and compared in constant time.

```json
{
  "language": "python",
  "code": "import pandas as pd\ndf = pd.read_csv('/work/in/sales.csv')\ndf.describe().to_excel('/work/out/summary.xlsx')\nprint(len(df))",
  "files": [{"name": "sales.csv", "content": "<base64>"}],
  "timeoutSec": 60
}
```

- `language` is `python` or `bash`. The code is written to `/work/main.py` or `/work/main.sh` and run with `python3` or `bash`, with `/work` as the working directory.
- Input files land in `/work/in`. Names are cut to their basename; `..`, empty names, control characters and names over 128 bytes are rejected.
- Top-level regular files the code leaves in `/work/out` come back. Directories, symlinks and anything over the limits are listed in `filesDropped` with a reason.

The answer, when the run itself happened (whatever its exit code):

```json
{
  "exitCode": 0, "timedOut": false, "durationMs": 3294, "peakMemoryMb": 194.1,
  "stdout": "500000\n", "stderr": "", "stdoutTruncated": false, "stderrTruncated": false,
  "files": [{"name": "summary.xlsx", "size": 12479, "content": "<base64>"}],
  "filesDropped": [{"name": "plots", "reason": "a directory; only top-level files in /work/out come back"}]
}
```

- A run that hits its timeout reports `timedOut: true` and exit code 124.
- A run killed for going over its memory reports exit code 137, with a note at the end of `stderr`.
- stdout and stderr keep their first and last bytes, with a marker in the middle saying how much was cut.

Errors are `{"error": "<code>", "message": "..."}`:

| Status | Codes |
|---|---|
| 400 | `invalid_json`, `invalid_language`, `invalid_code`, `invalid_timeout`, `invalid_file`, `too_many_files` |
| 401 | `unauthorized` |
| 413 | `body_too_large`, `code_too_large`, `file_too_large`, `files_too_large` |
| 429 | `busy` (with `Retry-After`) |
| 500 | `internal` (the message carries the run id; details stay in the log) |
| 503 | `not_ready` |

There are no other endpoints.

## Limits

| What | Limit |
|---|---|
| Code | 100 KB |
| Input files | 20 files, 15 MiB each, 50 MB in total |
| Request body | about 68 MiB, checked before decoding |
| Output files | 10 files from `/work/out`, 15 MiB each |
| stdout, stderr | 32 KB each (head and tail) |
| Timeout | 60 s default, 300 s max |
| Memory | 1 GiB, no swap (`memory.max`, `memory.swap.max 0`) |
| CPU | one CPU (`cpu.max 100000 100000`) |
| Host pids | 256 (`pids.max`; gVisor's own threads count here too) |
| Processes inside | 64 (`RLIMIT_NPROC`, enforced by gVisor with a clean EAGAIN) |
| Open files | 1024 (`RLIMIT_NOFILE`) |
| `/tmp` | 256 MB tmpfs inside the sandbox |
| `/work` | 512 MB tmpfs on the host, per run |
| Network | none (own network namespace, `runsc --network=none`) |
| Concurrency | 2 runs at once, 10 waiting, then 429 |
| Large requests | 2 in flight with a body over 1 MiB (as many as run slots), then 429. Small ones queue as usual |

## What a run looks like

1. The request is validated, then waits for one of the two run slots.
2. Kandang mounts a 512 MB tmpfs for the run, writes the script, `in/` and an empty `out/` into it (owned by uid 10001), and a writable copy of the image's matplotlib font cache.
3. It writes the OCI `config.json`: the shared rootfs read-only, tmpfs `/tmp`, the run's dir at `/work`, uid and gid 10001, no capabilities, `noNewPrivileges`, its own pid, network, ipc, uts and mount namespaces, hostname `kandang`.
4. It creates `/sys/fs/cgroup/kandang/<runID>` with the limits above and starts `runsc --ignore-cgroups --network=none --platform=systrap run` straight into that cgroup (`clone3` with `CLONE_INTO_CGROUP`), so the sandbox and everything it starts is capped from its first instruction.
5. On timeout it kills the whole cgroup (`cgroup.kill`) and runs `runsc kill`.
6. It reads `memory.peak`, the OOM count and `/work/out`, then always cleans up: `runsc delete --force`, `cgroup.kill`, rmdir the cgroup, unmount the tmpfs, remove the run dir. Whatever a crash left behind is swept at the next start.

## Tests

Unit tests run anywhere, macOS included, without `runsc`:

```bash
cd apps/server
gofmt -l .        # prints nothing
go vet ./...
go test ./...
```

The integration tests run real jobs through the real runner (a pandas chart, a 2 GB allocation, a fork loop, a busy loop, a socket connect, files in and out, two runs at once). They need Linux, root, `runsc` and the rootfs, so run them in a throwaway isolated OrbStack machine:

```bash
deploy/machine-setup.sh kandang-it
deploy/deploy.sh kandang-it          # unpacks the rootfs into the machine
cd apps/server
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -tags integration -o /tmp/kandang-it.test ./internal/sandbox
orb -m kandang-it -u root bash -c 'cat > /root/kandang-it.test && chmod 755 /root/kandang-it.test' < /tmp/kandang-it.test
orb -m kandang-it -u root bash -c 'systemctl stop kandang && /root/kandang-it.test -test.v'
orb delete -f kandang-it
```

## Deploy

Two scripts in `deploy/`, both run from the Mac:

1. `deploy/machine-setup.sh [machine]` creates the isolated machine (Debian 13, 3 GB, 2 CPUs) or brings an existing one up to date. Inside it installs `runsc` from gVisor's signed apt repo, loads an nftables rule that drops outbound traffic to private and link-local ranges (replies to incoming connections, IPv6 neighbor discovery and DHCP renewals still pass), creates the `kandang` user (uid 10001, the sandbox uid) and the dirs, generates the bearer token in `/etc/kandang/token` (mode 0600, never printed) and installs `kandang.service`. Safe to run again.
2. `deploy/deploy.sh [machine]` cross-compiles `kandang` for linux/arm64, builds `image/Dockerfile` with the Mac's Docker, exports a container to a tar and streams it into the machine through `orb -u root` (an isolated machine has no file sharing). It unpacks into a new `rootfs-<time>` dir, swaps the `rootfs` symlink in one rename, keeps the previous rootfs for a rollback, replaces the binary, restarts the service and prints `/health`. The image it built is removed afterwards.

To give a client the token, run `deploy/show-token.sh [machine]` yourself in a terminal. It's the only thing that ever prints it.

The service runs as root because it creates cgroups, mounts a tmpfs per run and starts `runsc`. The unit adds what hardening systemd allows without breaking `runsc` (`NoNewPrivileges`, `ProtectSystem=strict` with only `/var/lib/kandang` and `/sys/fs/cgroup` writable, `PrivateTmp`, `ProtectHome`, kernel module, log and clock protection, a memory cap for kandang itself).
