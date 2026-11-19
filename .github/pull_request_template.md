## What and why

<!-- One or two sentences: what this changes and the reason. Link the issue if there is one. -->

## How I checked it

<!-- Commands you ran and what they showed, e.g. go test ./..., a curl against /v1/runs, a screenshot for site changes. -->

## Checklist

- [ ] `gofmt -l .`, `go vet ./...` and `go test ./...` pass in `apps/server` (server changes)
- [ ] `pnpm --filter @kandang/site typecheck`, `test` and `build` pass (site changes)
- [ ] Docs updated if the API, limits or sandbox behavior changed
- [ ] No secrets, tokens or personal data in the diff
