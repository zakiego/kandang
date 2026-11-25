# Kandang

A throwaway Linux box for AI agents: run code in a gVisor sandbox, files in, files out.

[Website](https://kandang.zakiego.com) · [Docs](https://kandang.zakiego.com/docs)

![Kandang](https://kandang.zakiego.com/opengraph-image)

Your agent sends Python or bash plus some input files. Kandang runs them in a fresh gVisor sandbox with no network and sends back stdout, stderr and whatever the code wrote to `/work/out`. The sandbox, its cgroup and its files are gone when the run ends.

Kandang never holds a secret. Anything that needs an API key stays in your agent, which fetches the data first and passes it in as a file.

## Features

- One endpoint, `POST /v1/runs`, guarded by a bearer token. Plus an unauthenticated `GET /health`.
- An MCP server at `POST /mcp` (Streamable HTTP, stateless) with one `run` tool, so any MCP client can use it. Charts come back as images, other files as embedded resources.
- Every run gets its own gVisor sandbox: no network, read-only root, non-root user (uid 10001), no capabilities, `noNewPrivileges`, and its own pid, network, ipc, uts and mount namespaces.
- Hard caps per run through cgroup v2: 1 GiB memory with no swap, one CPU, 256 host pids, 64 processes inside, 60 s default and 300 s max timeout.
- Input and output files are base64 in JSON. Names are cut to a basename, and output symlinks are never followed.
- A small queue: 2 runs at once, 10 waiting, then `429` with `Retry-After`.
- The sandbox image ships Python with numpy, pandas, matplotlib, Pillow, openpyxl, pypdf and python-docx, so an agent can turn a CSV into a chart or a spreadsheet.

## How it works

```
agent ──POST /v1/runs──▶ kandang (Go, stdlib only)
                           │ validate, queue, stage /work/in on a tmpfs
                           │ write an OCI bundle, create a cgroup
                           ▼
                         runsc run --network=none   (gVisor sandbox)
                           │ code runs in /work, writes /work/out
                           ▼
                         collect outputs, kill the cgroup, clean up
agent ◀──exit code, stdout, stderr, files──┘
```

Kandang drives `runsc` directly. There's no Docker on the run path and no Docker socket anywhere. The process starts straight inside the run's cgroup (`clone3` with `CLONE_INTO_CGROUP`), so the limits apply from the first instruction. A timeout kills the whole cgroup.

## Stack

| Part | Path | Built with |
| --- | --- | --- |
| Server | `apps/server` | Go (standard library only, no cgo), gVisor `runsc`, cgroup v2 |
| Sandbox image | `image/` | Alpine, Python, pandas, matplotlib |
| Deploy | `deploy/` | Bash, OrbStack, systemd, nftables |
| Website and docs | `apps/site` | Next.js (static export), Fumadocs, Tailwind CSS |

## Quick start

The server needs Linux with gVisor and cgroup v2. On a Mac it runs in an isolated OrbStack machine that can't see the Mac's files.

```bash
deploy/machine-setup.sh            # create the isolated machine, install runsc, generate the token
deploy/deploy.sh                   # build the binary and the sandbox image, start the service
deploy/show-token.sh               # print the bearer token (in a terminal only)
```

Then send a run:

```bash
curl -s http://<host>:8080/v1/runs \
  -H "Authorization: Bearer $KANDANG_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"language":"python","code":"print(sum(range(10)))"}'
```

```json
{"exitCode": 0, "timedOut": false, "durationMs": 412, "peakMemoryMb": 21.3, "stdout": "45\n", "stderr": "", "stdoutTruncated": false, "stderrTruncated": false, "files": []}
```

The full request and response shapes, limits and error codes are in the [API docs](https://kandang.zakiego.com/docs/api).

To work on the website:

```bash
pnpm install
pnpm dev:site                      # http://localhost:4895
```

## Configuration

Every setting is a flag or an environment variable, and flags win. `kandang serve -h` lists them. The systemd unit reads overrides from `/etc/kandang/kandang.conf`, one `KEY=value` per line. [`deploy/kandang.conf.example`](deploy/kandang.conf.example) has every key with its default.

| Env | Flag | Default | What it does |
| --- | --- | --- | --- |
| `KANDANG_LISTEN` | `-listen` | `:8080` | Address to listen on |
| `KANDANG_TOKEN_FILE` | `-token-file` | `/etc/kandang/token` | File holding the bearer token. Must be mode 0600 or stricter and at least 32 characters |
| `KANDANG_RUNSC` | `-runsc` | `runsc` | Path to the `runsc` binary |
| `KANDANG_ROOTFS` | `-rootfs` | `/var/lib/kandang/rootfs` | Unpacked sandbox image, mounted read-only |
| `KANDANG_STATE_DIR` | `-state-dir` | `/var/lib/kandang/state` | runsc state and per-run dirs |
| `KANDANG_CGROUP_ROOT` | `-cgroup-root` | `/sys/fs/cgroup` | cgroup v2 mount |
| `KANDANG_MAX_RUNNING` | `-max-running` | `2` | Runs at once |
| `KANDANG_MAX_WAITING` | `-max-waiting` | `10` | Runs waiting for a slot before `429` |
| `KANDANG_MEMORY_MB` | `-memory-mb` | `1024` | Memory per run in MiB, no swap |
| `KANDANG_PIDS` | `-pids` | `256` | `pids.max` per run, gVisor threads included |
| `KANDANG_PROCS` | `-procs` | `64` | Processes per run inside the sandbox |
| `KANDANG_DEFAULT_TIMEOUT_SEC` | `-default-timeout-sec` | `60` | Timeout when a run doesn't set one |
| `KANDANG_MAX_TIMEOUT_SEC` | `-max-timeout-sec` | `300` | Largest timeout a run may ask for |

The token is never set through an env var. `deploy/machine-setup.sh` generates it inside the machine and only `deploy/show-token.sh` prints it.

## Deployment

The scripts in `deploy/` target an isolated OrbStack machine (Debian, arm64), but the server itself only needs Linux, `runsc` and cgroup v2:

1. Install `runsc` and make sure cgroup v2 is mounted.
2. Unpack the image built from `image/Dockerfile` into the rootfs dir.
3. Write a random token of at least 32 characters to the token file with mode 0600.
4. Install `deploy/kandang.service` and start it. `kandang check` verifies everything without writing anything.

Keep the API on a private network. It runs code for anyone with the token, so put it behind your own network boundary rather than on the open internet. The [self-hosting docs](https://kandang.zakiego.com/docs/self-hosting) cover the machine setup, the egress firewall and why the service runs as root.

## Development

```bash
cd apps/server
gofmt -l .
go vet ./...
go test ./...                      # unit tests, run anywhere without runsc
```

The integration tests need Linux, root, `runsc` and the rootfs. See `apps/server/README.md` for how to run them in a throwaway machine.

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md), and report security issues as described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
