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
```

Run a single test: `go test ./internal/hooks -run TestInstall`. Most packages have focused `_test.go` files (`hooks`, `ports`, `config`, `envfile`, `tui`).

Release: `goreleaser release --clean` (darwin arm64+amd64; the Homebrew tap stanza in `.goreleaser.yaml` is commented out until `mascah/homebrew-tap` exists).

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

- **`<repo>/.nerve/config.yaml`** (per-project, committed) — declares `services` (id, base_port, env_key, primary), `clone_files`, `templates`, lifecycle `hooks.post_create` / `pre_remove`, and `project.{port_offset, worktree_root, pool_size}`. A project with no `config.yaml` is "lightweight" — `nerve new` still works but only does a plain `git worktree add`.
- **`~/.config/nerve/projects.yaml`** (user-wide, gitignored from the repo) — maps logical project names to main-checkout paths. `XDG_CONFIG_HOME` is honored. Tests/walkthroughs override this to keep the real registry untouched.

`internal/config` owns both. `LoadProjectConfig` returns `ErrNotFound` when the file is missing — callers (e.g. `loadProjectConfigOrLightweight` in `internal/cli/common.go`) interpret that as lightweight mode and pass `Cfg: nil` to `worktree.Create`.

### Port allocator (the load-bearing invariant)

`internal/ports.Allocate` uses **offset arithmetic, not arbitrary port assignment**: for offset N in `[1, pool_size]`, each service's port is `service.base_port + project.port_offset + N`. This is deliberate — it preserves predictable URLs ("django for worktree 3 is always 8003"). Don't replace this with a free-port search: callers and users rely on the fact that the offset (and therefore every service's port) is stable for the lifetime of a worktree.

A short `net.Listen("tcp", "127.0.0.1:port")` probe rejects offsets where any service's port is bound by an external squatter. The `ProbeFunc` injection point exists so tests can stay hermetic — don't wire `ProbeBind` directly into tests.

The registry (`internal/registry`) is `<repo>/.nerve/ports.json`, guarded by a sibling flock (`ports.json.lock`). All mutating access goes through `Handle.With(func(*Registry) error)`, which acquires the exclusive lock, reads, runs the callback, and writes atomically (temp + rename). Read-only callers use `Handle.Read()`.

### The worktree lifecycle (the place to make changes carefully)

`internal/worktree.Create` is the single funnel for both `nerve new` and the `nerve worktree-create` hook. The order is load-bearing and the rollback step matters:

1. Compute target path from `worktree_root` template (`{branch}` / `{project}` substitution).
2. Ensure `.worktrees/` is in `.gitignore` (and `.nerve/ports.json` + `.nerve/*.lock` when configured).
3. `git worktree add` **first** — if this fails we haven't claimed a port yet.
4. Lightweight short-circuit: if `Cfg == nil` or no services, optionally copy `.worktreeinclude` files and return.
5. Open registry under exclusive lock, clean stale allocations, allocate ports. **If allocation fails, roll back the git worktree** — otherwise we leak a worktree with no allocation.
6. Build template vars (`branch`, `project`, `worktree_path`, `ports.<id>` for each service), copy `clone_files`, render `templates`, write `.env.local` with per-service `EnvKey=port` pairs, run `post_create` hooks.

`Remove` mirrors this in reverse: dirty/unpushed checks → `pre_remove` hooks → release port → `git worktree remove` → delete branch iff `CreatedByNerve` and not `KeepBranch`.

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

## Conventions worth knowing

- **Exit codes** are listed in `internal/cli/common.go` and `docs/TESTING.md`'s quick reference. Currently `cmd/nerve/main.go` exits 1 on any error; `exitCodeError` is plumbed through `nerve new` for `ErrPoolExhausted` but not yet honored by `main.go` — if you wire that through, update both ends.
- **Atomic writes** — both `internal/config.writeYAMLAtomic` and `internal/registry.writeAtomic` write to a sibling temp file and `os.Rename`. New persistence should follow this pattern.
- **Silent no-ops in hook contexts** — `nerve env --inject` deliberately returns exit 0 with no output when cwd isn't in a registered + configured worktree, because it's wired into `SessionStart` / `CwdChanged` and surfacing errors would noise up every Claude session. Keep that contract.
- **Hidden hook commands** — `worktree-create` and `worktree-remove` are `Hidden: true` on the cobra command. They're not for humans.
