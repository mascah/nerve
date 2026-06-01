# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`nerve` is a Go CLI that manages git worktrees for projects with multiple network-bound services (Django, Postgres, Vite, etc.) and integrates with Claude Code's `WorktreeCreate` / `WorktreeRemove` / `SessionStart` / `CwdChanged` hooks. Its job is to make `claude --worktree <branch>` Just Work: create the worktree under `<repo>/.worktrees/<branch>/`, allocate non-conflicting ports for every service, copy untracked dotfiles (`.env`, `.npmrc`), and inject port env vars into the Claude Code session.

It is single-binary, single-user, Go-only (no cgo). Module path: `github.com/mascah/nerve`. Requires Go 1.22+ per the README, though `go.mod` currently pins 1.26.2.

## Commands

```bash
make build           # → bin/nerve
make install         # → $GOPATH/bin/nerve (used for local dogfooding)
make test            # go test ./...
make vet             # go vet ./...
make fmt             # gofmt -s -w .
make tidy            # go mod tidy
make dev ARGS="..."  # go run ./cmd/nerve <args>
make lint            # golangci-lint run (falls back to go vet if not installed)
make hooks           # install lefthook git hooks (needs the lefthook binary)
```

Run a single test: `go test ./internal/hooks -run TestInstall`. Most packages have focused `_test.go` files (`hooks`, `ports`, `config`, `envfile`, `tui`).

**Lint + git hooks.** `.golangci.yml` configures golangci-lint v2 (the standard set: errcheck/govet/ineffassign/staticcheck/unused, with errcheck excluding fire-and-forget I/O). `lefthook.yml` defines the **pre-commit** hook — the fast static checks: gofmt/vet/golangci-lint/build. CI (`.github/workflows/ci.yml`) runs those same jobs via `lefthook run pre-commit --all-files` (so local and CI lint/build can't drift) and then runs the test suite with `go test -race`. Tests deliberately stay out of git hooks — kept fast, and not run inside git's hook environment. Dev setup: install `golangci-lint` and `lefthook` (both `go install`-able; versions pinned in the CI workflow's `env:`), then `make hooks`.

Release: fully automated via `.github/workflows/release.yml` (release-please + goreleaser, conventional-commit driven). On push to `main`, release-please maintains a release PR that bumps the version and rewrites `CHANGELOG.md`; merging it creates the `vX.Y.Z` tag + GitHub Release, and in the *same* job goreleaser builds darwin arm64+amd64 archives and attaches them to that release. release-please owns the changelog/release notes; goreleaser's own changelog is disabled and `release.mode: keep-existing` keeps it from clobbering the notes — this resolves the "two tools, one changelog" overlap. No PAT needed (both run as steps of one job, since a `GITHUB_TOKEN`-pushed tag won't trigger a separate `on: push: tags` workflow). After a release, bump `url`+`sha256` in the [mascah/homebrew-tap](https://github.com/mascah/homebrew-tap) formula (`brew install mascah/tap/nerve` builds from source — no signing). release-please config lives in `release-please-config.json` + `.release-please-manifest.json` (manifest mode, `release-type: go` — version lives only in the git tag and is injected via ldflags, so no in-repo version file is bumped).

## End-to-end smoke

`docs/TESTING.md` is the canonical walkthrough — when changing worktree/port/hook behavior, replay the relevant sections against a throwaway sandbox under `$XDG_CONFIG_HOME=$SANDBOX/.config` so it doesn't mutate the real `~/.config/nerve/projects.yaml`.

## Architecture

### Entry points (two-headed: human CLI and Claude Code hooks)

`cmd/nerve/main.go` is a one-line dispatcher to `internal/cli.NewRootCmd()`. The same Go entry point services both audiences:

- **Human-facing subcommands** — `init`, `project add/list/remove`, `new`, `remove`, `list`, `env`, `ports`, `hooks`, `refresh`, `doctor`, `version`. Defined in `internal/cli/*.go`.
- **Hook-facing subcommands** (hidden, stdin-driven, registered by `nerve hooks install`):
  - `nerve worktree-create` — reads `{"name": ..., "cwd": ...}` JSON from stdin, looks up the project, creates the worktree, prints the absolute path to stdout (Claude Code consumes this as the worktree path).
  - `nerve worktree-remove --from-hook` — reads `{"path": ..., "name": ...}` from stdin, runs cleanup.
  - `nerve env --inject` — appends per-worktree port env vars to `$CLAUDE_ENV_FILE` so the Bash tool in the session sees them. Silent no-op outside a registered worktree.

`nerve` with no args launches `internal/tui` (bubbletea-based project setup TUI).

### Two layers of config

- **`<repo>/.nerve/config.yaml`** (per-project, committed) — declares `services` (id, base_port, env_key, primary), `vars` (static `env_key`/`value` pairs written to `.env.local`, `value` templated), `clone_files`, `templates`, lifecycle `hooks.post_create` / `pre_remove`, and `project.{port_offset, worktree_root, pool_size, background_post_create, background_remove}`. **`post_create` hooks are per-command foreground or background** (see `internal/config.HookCommand`): a bare string is foreground (sync, sequential, blocks boot, failure rolls back the worktree); `{run: ..., background: true}` is background (detached, runs concurrently with other background hooks, failure recorded not rolled back). Foreground is the default so env-shapers like `direnv allow` take effect before a session starts. The project-level `background_post_create` is a **deprecated** fallback default for hooks that don't set their own `background:`. `background_remove` (opt-in, default false) makes teardown return instantly by renaming the dir into `.nerve/trash/` and deleting it detached. A project with no `config.yaml` is "lightweight" — `nerve new` still works but only does a plain `git worktree add`.
- **`~/.config/nerve/projects.yaml`** (user-wide, gitignored from the repo) — maps logical project names to main-checkout paths. `XDG_CONFIG_HOME` is honored. Tests/walkthroughs override this to keep the real registry untouched.

`internal/config` owns both. `LoadProjectConfig` returns `ErrNotFound` when the file is missing — callers (e.g. `loadProjectConfigOrLightweight` in `internal/cli/common.go`) interpret that as lightweight mode and pass `Cfg: nil` to `worktree.Create`.

### Port allocator (the load-bearing invariant)

`internal/ports.Allocate` uses **offset arithmetic, not arbitrary port assignment**: for offset N in `[1, pool_size]`, each service's port is `service.base_port + project.port_offset + N`. This is deliberate — it preserves predictable URLs ("django for worktree 3 is always 8003"). Don't replace this with a free-port search: callers and users rely on the fact that the offset (and therefore every service's port) is stable for the lifetime of a worktree.

A short `net.Listen("tcp", "127.0.0.1:port")` probe rejects offsets where any service's port is bound by an external squatter. The `ProbeFunc` injection point exists so tests can stay hermetic — don't wire `ProbeBind` directly into tests.

The registry (`internal/registry`) is `<repo>/.nerve/ports.json`, guarded by a sibling flock (`ports.json.lock`). All mutating access goes through `Handle.With(func(*Registry) error)`, which acquires the exclusive lock, reads, runs the callback, and writes atomically (temp + rename). Read-only callers use `Handle.Read()`.

A second store, the **cross-project leases** file at `~/.config/nerve/ports.json` (user-wide, `XDG_CONFIG_HOME` honored — note the same basename as the per-project registry, but a different directory and schema), keeps two *different* projects from binding the same host port. `internal/leases` mirrors the registry's flock + atomic-write discipline. During `Create`, after the per-project registry allocation succeeds, the chosen ports are `Reserve`d in the leases store; a `LeaseChecker` is also passed into `ports.Allocate` as a pre-check so the allocator skips offsets another project already holds. **Lock order is load-bearing: per-project registry first, then the global leases store** — reversing it can deadlock concurrent `nerve new` calls across projects. `Remove` releases the lease.

### The worktree lifecycle (the place to make changes carefully)

`internal/worktree.Create` is the single funnel for both `nerve new` and the `nerve worktree-create` hook. The order is load-bearing and the rollback step matters:

1. Compute target path from `worktree_root` template (`{branch}` / `{project}` / `{branch_slug}` substitution).
2. Ensure `.worktrees/` is in `.gitignore` (and `.nerve/ports.json` + `.nerve/*.lock` + `.nerve/hooks/` + `.nerve/trash/` when configured).
3. `git worktree add` **first** — if this fails we haven't claimed a port yet.
4. Lightweight short-circuit: if `Cfg == nil` or no services, optionally copy `.worktreeinclude` files and return.
5. Open registry under exclusive lock, clean stale allocations, allocate ports. **If allocation fails, roll back the git worktree** — otherwise we leak a worktree with no allocation.
6. Build template vars (`branch`, `project`, `worktree_path`, `branch_slug`, `ports.<id>` for each service), copy `clone_files`, render `templates`, write `.env.local` with per-service `EnvKey=port` pairs, run `post_create` hooks. `branch_slug` is the branch name lowered to `[a-z0-9_]` (via `config.Slugify`) for identifier-safe Docker/Postgres/etc. names; an all-punctuation branch errors before `git worktree add`. Hooks are partitioned via `HookCommands.Partition` into **foreground** (run inline here by `RunHooks`, sequentially — a failure rolls back the worktree) and **background** (when any exist, Create writes a `running` marker via `internal/hookstatus` and spawns a detached `nerve run-hooks` child — `spawnDetached`, Setsid — which re-derives the background subset and runs it *concurrently* via `RunHooksParallel`, recording `ok`/`failed` under `.nerve/hooks/<branch_slug>/`). Everything else (ports, clone, templates, `.env.local`) and the foreground hooks stay synchronous, so the worktree is structurally complete and its env settled before the path is printed.

`Remove` mirrors this in reverse: dirty/unpushed checks (skipped when `Force`) → `pre_remove` hooks → release port → **`chdirAwayIfInside` (never delete the cwd out from under a live process — that's what hung the TUI)** → remove the worktree dir → clear hook status → delete branch iff `CreatedByNerve` and not `KeepBranch`. The dir removal goes through `removeWorktreeDir` (`internal/worktree/teardown.go`): a synchronous `git worktree remove` by default, or — when `background_remove` is set — `os.Rename` into `.nerve/trash/` + a synchronous `git worktree prune` (so git's view never lags) + a detached `nerve gc-trash` that deletes the bytes. The rename falls back to synchronous `git worktree remove` if it can't be done atomically (cross-filesystem `worktree_root`). The TUI calls `Remove` from a `tea.Cmd` goroutine so the UI never blocks regardless of which path runs.

### Hook installer

`internal/hooks` writes/edits `.claude/settings.json` for the Claude Code integration. Every nerve-managed command string is tagged with the literal sentinel `# nerve-managed` so `Uninstall` can find and remove only nerve entries while preserving any other hooks in the file. Treat the sentinel as load-bearing — don't change the string without updating both `Snippet()` and `isNerveCommand()`.

The four events nerve registers:

| Event | Command | Why |
|---|---|---|
| `WorktreeCreate` | `nerve worktree-create` | Replaces Claude's default git logic; routes through nerve so ports + clone files happen. |
| `WorktreeRemove` | `nerve worktree-remove --from-hook` | Runs `pre_remove` hooks and releases the port. |
| `SessionStart` | `nerve env --inject` | Appends port env vars to `$CLAUDE_ENV_FILE` on session start. |
| `CwdChanged` | `nerve env --inject` | Re-injects when the user `cd`s between worktrees mid-session. |

### Discovery is git-driven

`internal/gitutil.Discover` resolves any path to `{MainCheckout, CurrentWorktree, CommonGitDir, IsWorktree}` via `git rev-parse --show-toplevel --git-common-dir`. The main checkout is `filepath.Dir(commonDir)`. This is how `nerve env --inject` figures out which worktree it's in regardless of where the user `cd`s. **`.nerve/` and `.nerve/ports.json` always live in the main checkout**, never in a linked worktree — `gitutil.Discover` is the only correct way to find that path.

### TUI

`internal/tui` is a bubbletea app with a root `App` that holds the current `viewKey` and delegates `Update`/`View` to one of the sub-views (`projectsView`, `addProjectView`, `projectView`, `serviceForm`, `cloneForm`). Navigation between views is by `switchViewMsg{to, payload}`. The TUI mutates `.nerve/config.yaml` directly (via `internal/config`) — there's no separate in-memory model.

**The event loop must never block.** Anything that forks `git`, reads the flock'd registry, or calls `worktree.Remove` runs inside a `tea.Cmd` goroutine that delivers a result message (`worktreesLoadedMsg`, `worktreeRemovedMsg`) handled in `projectView.Update` *before* the `tea.KeyMsg` type-assertion. The Worktrees list loads eagerly when the project view opens (so the `(N)` count shows without tabbing in), shows a `loading…` placeholder until `worktreesLoadedMsg` arrives, and the per-row hook status comes from `hookstatus.Read`. `Run(cwd string)` does the one and only synchronous git work at startup — `detectCurrentWorktree(cwd)` (Discover + `envinject.Compute`) — to render the current worktree's ports as a header. Removal is two-press confirm (the prompt names the dirty-file count) then an async `worktree.Remove`. Don't reintroduce synchronous I/O in `Update`/`View`.

## Conventions worth knowing

- **Exit codes** are listed in `internal/cli/common.go` and `docs/TESTING.md`'s quick reference. `cmd/nerve/main.go` maps the root command's error to a code via `cli.ExitCode`: a `nil` error → `ExitOK`, an `exitCodeError` → its `Code` (e.g. `nerve new` returns `ExitPoolExhausted` on `ErrPoolExhausted`), any other error → `ExitUsage` (1). To add a new coded exit, return an `exitCodeError{Code, Err}` from the relevant `RunE` — `main.go` needs no change.
- **Atomic writes** — all bytes-to-disk persistence goes through `internal/atomicfile.Write(path, data, perm)`, which writes a sibling temp file in the destination dir and `os.Rename`s it into place (`internal/config`, `internal/jsonstore` (registry+leases), `internal/hookstatus`, `internal/envfile`, and worktree template rendering all call it). New persistence should reuse it rather than re-rolling temp+rename. (`internal/clone.copyFile` keeps its own streaming temp+rename since it `io.Copy`s rather than holding bytes in memory.)
- **Silent no-ops in hook contexts** — `nerve env --inject` deliberately returns exit 0 with no output when cwd isn't in a registered + configured worktree, because it's wired into `SessionStart` / `CwdChanged` and surfacing errors would noise up every Claude session. Keep that contract.
- **Hidden hook commands** — `worktree-create`, `worktree-remove`, `run-hooks`, and `gc-trash` are `Hidden: true` on the cobra command. They're not for humans: `run-hooks` is the detached post_create runner (writes `internal/hookstatus`), `gc-trash` empties `.nerve/trash/` (also sweeping leftovers from an interrupted delete).
- **Detached background work** — `worktree.spawnDetached` (`spawn_unix.go`, `Setsid`; a `!unix` stub returns "unsupported" so callers fall back to synchronous) re-execs the nerve binary fully detached with std streams to `/dev/null` — never inherit the parent's stdout, since the WorktreeCreate hook reads the worktree path from it. The package var `spawnDetachedFn` is the injection point for hermetic tests (mirrors `ports.ProbeFunc`).
- **`internal/hookstatus`** is the shared contract for backgrounded-hook state under `.nerve/hooks/<slug>/` (`status.json` written atomically + `log`). Writer: `nerve run-hooks`. Readers: the TUI Worktrees tab and `nerve list`. `slug` is always `config.Slugify(branch)` on both sides.
