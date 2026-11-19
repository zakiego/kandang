# Contributing

Bug reports, fixes and small improvements are welcome. For anything bigger, open an issue first so we can agree on the approach before you spend time on it.

## Layout

- `apps/server`: the Go server. Standard library only, so please don't add dependencies.
- `image/`: the sandbox image.
- `deploy/`: machine setup and deploy scripts.
- `apps/site`: the website and docs (Next.js, Fumadocs).

## Before you open a PR

For the server:

```bash
cd apps/server
gofmt -l .        # should print nothing
go vet ./...
go test ./...
```

For the site:

```bash
pnpm install
pnpm --filter @kandang/site typecheck
pnpm --filter @kandang/site test
pnpm --filter @kandang/site build
```

CI runs the same checks on every PR.

## Commits and PRs

- Keep each commit to one change, and use [Conventional Commits](https://www.conventionalcommits.org/): `fix(sandbox): reject names with a NUL byte`.
- Add or update tests for behavior changes.
- If a change touches the sandbox, its limits or the API, update the docs in `apps/site/content/docs` and the server README in the same PR.

Found a security issue? Don't open a PR or issue. Follow [SECURITY.md](SECURITY.md).
