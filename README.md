<!-- TODO: demo gif/asciinema — see issue -->
<!-- Caption: two parallel worktrees (feat-auth, feat-payments) each with Django, Postgres, and Vite
     running on deterministic, non-conflicting port sets — no manual config required. -->

# nerve

[![CI](https://github.com/mascah/nerve/actions/workflows/ci.yml/badge.svg)](https://github.com/mascah/nerve/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Runtime isolation for git worktrees.** Give every parallel worktree its own non-conflicting ports, env files, and dotfiles — for any AI coding agent (Claude Code, Codex, Cursor, …) or plain manual use.

## The problem

Git worktrees let you run multiple branches in parallel. Great. Except the moment you spin up `feat-auth` and `feat-payments` side by side, both want port 8000 for Django, 5432 for Postgres, and 5173 for Vite. Both need a `.env`. Tools that create worktrees punt on the runtime environment — you're left patching ports by hand, or writing fragile scripts that break across machines.

## What nerve does

nerve solves the environment problem. For each new worktree it:

1. **Allocates a stable, unique port set** — offset-based, not random. Worktree 3's Django is always `8003`. URLs are predictable; bookmarks don't break.
2. **Copies dotfiles** (`.env`, `.npmrc`, etc.) you declare in `clone_files`.
3. **Renders templates** with per-worktree port vars so every worktree gets its own `.env.local`.
4. **Runs bootstrap hooks** (`post_create`) — `uv sync`, `pnpm install`, `bundle install` — instead of copying gigantic directories between worktrees.
5. **Registers the allocation** in a flock-guarded registry so two concurrent `nerve new` calls never collide, and ports held by other projects in your registry are avoided automatically.

The harness-agnostic core (`nerve new`) works standalone. A first-class Claude Code integration is available separately (see [Integrations](#integrations)).

## Differentiator: stable, offset-based ports

For offset N in `[1, pool_size]`, each service's port is `base_port + project_offset + N`. This is deliberate:

- **Predictable URLs.** Worktree 3's services are always at offset 3 — bookmark `http://localhost:8003`, share it with a teammate, and it still works tomorrow.
- **No port thrash.** Restarting a worktree or re-running `nerve env` doesn't reassign ports.
- **Cross-project isolation.** The registry tracks leases across all your nerve-managed repos, so a port claimed by `project-a:worktree-2` is never given to `project-b:worktree-5`.
- **Squatter detection.** Before finalising an offset, nerve probes each candidate port with a short `net.Listen`. An offset where any service port is already bound by an external process is skipped.

## Install

**Build from source** (requires Go 1.26+):

```bash
git clone https://github.com/mascah/nerve
cd nerve
make install   # → $GOPATH/bin/nerve
```

**Homebrew** *(coming soon — no tap exists yet; use build-from-source for now)*:

```bash
# brew install mascah/tap/nerve   # not yet available
```

**Download a release archive:**

```
https://github.com/mascah/nerve/releases
```

## Quick start

```bash
# Register your repo with nerve
nerve project add ~/Code/my-app

# Scaffold .nerve/config.yaml (interactive)
cd ~/Code/my-app && nerve init

# Create a worktree — nerve allocates ports, copies dotfiles, runs post_create hooks
nerve new my-app feat-foo

# Inspect what was allocated
nerve list my-app

# See the env vars for the current worktree
nerve env
```

## Layout

| Path | Purpose |
|---|---|
| `<repo>/.nerve/config.yaml` | Per-project config: services, clone files, templates, lifecycle hooks |
| `<repo>/.nerve/ports.json` | Port allocation registry (gitignored, flock-guarded) |
| `<repo>/.worktrees/<branch>/` | Worktrees live here (gitignored) |
| `~/.config/nerve/projects.yaml` | Global project registry (respects `$XDG_CONFIG_HOME`) |

## Config schema (`<repo>/.nerve/config.yaml`)

```yaml
project:
  port_offset: 0       # base offset added to every service port for this project
  pool_size: 20        # max simultaneous worktrees
  worktree_root: ".worktrees/{branch}"

services:
  - id: django
    base_port: 8000
    env_key: DJANGO_PORT
    primary: true
  - id: postgres
    base_port: 5432
    env_key: DATABASE_PORT
  - id: vite
    base_port: 5173
    env_key: VITE_PORT

# Files copied verbatim from main checkout into each new worktree
clone_files:
  - .env
  - .npmrc

# Templates rendered with per-worktree vars ({branch}, {project}, ports.<id>)
templates:
  - src: .env.local.tmpl
    dst: .env.local

hooks:
  post_create:
    - uv sync
    - pnpm install
  pre_remove: []
```

Available template variables: `{branch}`, `{project}`, `{worktree_path}`, `{ports.<service-id>}`.

**Lightweight mode:** if `.nerve/config.yaml` is absent, `nerve new` still creates a plain git worktree with no port allocation or file copying.

## Commands

| Command | Purpose |
|---|---|
| `nerve init` | Scaffold `.nerve/config.yaml` interactively |
| `nerve project add/list/remove` | Manage global project registry |
| `nerve new <project> <branch>` | Create worktree: allocate ports, copy files, run hooks |
| `nerve remove [<project>] [<branch>]` | Remove worktree: release port, optionally delete branch |
| `nerve list [<project>]` | List active worktrees and their allocated ports |
| `nerve env` | Print per-worktree port env vars for current directory |
| `nerve ports list/cleanup/status` | Inspect the port registry |
| `nerve gc [<project>]` | Clear leftover bytes in `.nerve/trash` (from an interrupted `background_remove`) |
| `nerve refresh` | Re-render templates + env in the current worktree |
| `nerve doctor` | Diagnose config and registry |
| `nerve hooks install` | Wire nerve into Claude Code (see Integrations) |
| `nerve version` | Print nerve version |

`nerve` with no arguments launches the interactive TUI project-setup interface.

## Integrations

### Claude Code

`nerve hooks install` writes four hook entries into Claude Code's settings file, making `claude --worktree <branch>` from a nerve-registered repo Just Work:

| Hook event | Command | Purpose |
|---|---|---|
| `WorktreeCreate` | `nerve worktree-create` | Replaces Claude's default git logic; nerve creates the worktree, allocates ports, copies clone files, and prints the absolute path back to Claude Code |
| `WorktreeRemove` | `nerve worktree-remove --from-hook` | Runs pre-remove hooks, releases the port, deletes the worktree and branch |
| `SessionStart` | `nerve env --inject` | Appends per-worktree port env vars to `$CLAUDE_ENV_FILE` so Bash tool calls see them |
| `CwdChanged` | `nerve env --inject` | Re-injects when the user `cd`s between worktrees mid-session |

After install, `claude --worktree feat-foo` creates the worktree at `<repo>/.worktrees/feat-foo/` (instead of Claude's default `<repo>/.claude/worktrees/`), with ports allocated and env vars live in the session.

#### Fast boot & teardown (opt-in)

For large projects, `post_create` installs (`uv sync`, `pnpm i`) and the recursive delete of `node_modules`/`.venv` on teardown can each take 30+ seconds, and `claude --worktree` blocks on them. You can move that work off the critical path per command and per project.

**Per-command background hooks.** By default every `post_create` hook is **foreground**: it runs synchronously, in order, before the worktree path is reported — so env-shapers like `direnv allow` are in effect before a session starts. Tag a hook with `background: true` to detach it; multiple background hooks run **concurrently**:

```yaml
hooks:
  post_create:
    - direnv allow            # foreground: runs first, blocks boot (correct for env-shapers)
    - run: uv sync
      background: true        # detached
    - run: pnpm install
      background: true        # detached → runs in parallel with uv sync
```

Background hooks run with the allocated ports + identity vars in their environment (same as the synchronous path). Progress and a terminal `running`/`ok`/`failed` status are written under `.nerve/hooks/<branch_slug>/`; `nerve list` and the TUI Worktrees tab show a `HOOKS` column so you can tell when bootstrap finished. A failing background hook is recorded as `failed` (it can't roll back an already-created worktree); a failing foreground hook aborts create and rolls the worktree back. Note the trade-off: an agent could start a dev server before a background `pnpm i` has finished — keep such prerequisites foreground.

> The project-wide `project.background_post_create: true` flag is **deprecated** but still honored as a fallback default for hooks that don't set their own `background:`. Prefer the per-command form above.

**Background teardown** is a separate per-project flag (default `false`):

```yaml
project:
  background_remove: true   # return from teardown immediately; trash + delete the worktree dir in the background
```

- **`background_remove`** — teardown renames the worktree into `.nerve/trash/` (instant) and deletes the bytes in a detached process. git's own metadata is reconciled *synchronously* (`git worktree prune`), so its view never goes out of sync even if the background delete is interrupted; leftovers are swept on the next remove. Falls back to a synchronous delete if the worktree lives on a different filesystem than `.nerve/`. If a detached delete is ever interrupted, `nerve doctor` reports the leftover bytes and `nerve gc` clears them on demand.

#### Install scope

Because not everyone on your team will have nerve installed, `nerve hooks install --project` writes to the **per-user local override** (gitignored) by default:

| Flag | Target file |
|---|---|
| `--project` *(default)* | `<repo>/.claude/settings.local.json` — user-local, gitignored; nerve adds it to `.gitignore` on first install |
| `--shared` | `<repo>/.claude/settings.json` — committed; use only if every collaborator has nerve |
| `--user` | `~/.claude/settings.json` — applies to every repo on your machine |

#### Merge semantics

`nerve hooks install` merges into the target settings file — it never overwrites it. Each nerve-managed command is tagged with the sentinel `# nerve-managed`, and `nerve hooks uninstall` removes only entries carrying that sentinel. Re-running install is idempotent. When you have your own hook for an event nerve also uses (e.g. `SessionStart`), both coexist — nerve appends alongside, it does not replace.

## Releasing

```bash
goreleaser release --clean
```

Builds darwin arm64 + amd64 binaries. The Homebrew tap stanza in `.goreleaser.yaml` is commented out until `mascah/homebrew-tap` is published.
