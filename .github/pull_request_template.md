## What and why

<!-- What this changes and the problem it solves. Link the issue if there is one. -->

## How it was checked

- [ ] `go test ./...`, `go vet ./...`, `golangci-lint run`, `scripts/check-core-imports.sh`
- [ ] `npm --prefix frontend run typecheck` and `npm --prefix frontend test`
- [ ] `wails3 task test:e2e` (UI changes)
- [ ] Manual checks on a Mac, if the change touches the tray, menus, notifications or windows (see `docs/testing.md`)
- [ ] `CHANGELOG.md` updated
