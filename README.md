# nerve

Worktree manager with Claude Code integration. Replaces hand-rolled worktree scripts and integrates with Claude Code's `WorktreeCreate` / `WorktreeRemove` hooks. Per-project `hooks.post_create` commands run after clone files and templates are in place — use them for `uv sync`, `pnpm install`, `bundle install`, and similar bootstrap steps rather than copying `.venv` / `node_modules` between worktrees.

## Install

```bash
# From source (requires Go 1.22+):
make install

# Once a homebrew tap is published:
# brew install mascah/tap/nerve

# Or grab a release archive from GitHub:
# https://github.com/mascah/nerve/releases
```

## Quick start

```bash
# Build & install
make install

# Register an existing repo
nerve project add ~/Code/my-app

# Configure services + clone files (interactive)
cd ~/Code/my-app && nerve init

# Create a worktree
nerve new my-app feat-foo

# Install Claude Code hooks (per-repo or user-wide)
nerve hooks install --project
```

## Layout

- `<repo>/.nerve/config.yaml` — per-project config (services, clone files, lifecycle hooks)
- `<repo>/.nerve/ports.json` — port allocation registry (gitignored)
- `<repo>/.worktrees/<branch>/` — worktrees live here (gitignored)
- `~/.config/nerve/projects.yaml` — global project registry

## Commands

| Command | Purpose |
|---|---|
| `nerve init` | Scaffold `.nerve/config.yaml` |
| `nerve project add/list/remove` | Manage global project registry |
| `nerve new <project> <branch>` | Create worktree |
| `nerve remove [<project>] [<branch>]` | Remove worktree |
| `nerve list [<project>]` | List worktrees + allocated ports |
| `nerve env --inject` | Append per-worktree env to `$CLAUDE_ENV_FILE` |
| `nerve ports list/cleanup/status` | Inspect port registry |
| `nerve gc [<project>]` | Clear leftover bytes in `.nerve/trash` (from an interrupted `background_remove`) |
| `nerve hooks install` | Wire into Claude Code |
| `nerve worktree-create` / `nerve worktree-remove --from-hook` | Stdin-driven entry points for Claude Code hooks |
| `nerve refresh` | Re-render templates + env in cwd worktree |
| `nerve doctor` | Diagnose config + registry |

## Claude Code integration

`nerve hooks install --project` writes the following into `<repo>/.claude/settings.local.json` (the per-user override file — see [Install scope](#install-scope-which-settings-file) below):

| Hook event | Command | Purpose |
|---|---|---|
| `WorktreeCreate` | `nerve worktree-create` | Replaces Claude's default git logic; nerve creates the worktree, allocates ports, copies clone files, and prints the path back to Claude Code |
| `WorktreeRemove` | `nerve worktree-remove --from-hook` | Runs pre-remove hooks, releases the port, deletes the worktree + branch |
| `SessionStart` + `CwdChanged` | `nerve env --inject` | Appends per-worktree port env vars to `$CLAUDE_ENV_FILE` so Bash tool calls see them |

After install, `claude --worktree feat-foo` from a nerve-registered repo will create the worktree at `<repo>/.worktrees/feat-foo/` (instead of Claude's default `<repo>/.claude/worktrees/`), with ports allocated and env vars wired up for the session.

### Fast boot & teardown (opt-in)

For large projects, `post_create` installs (`uv sync`, `pnpm i`) and the recursive delete of `node_modules`/`.venv` on teardown can each take 30+ seconds, and `claude --worktree` blocks on them. Two per-project flags move that work off the critical path. Both default to `false` (fully synchronous — the env is guaranteed ready before the path is printed, and teardown is complete when the command returns); enable them per project under `project:` in `.nerve/config.yaml`:

```yaml
project:
  background_post_create: true   # print the worktree path immediately; run post_create hooks in a detached process
  background_remove: true         # return from teardown immediately; trash + delete the worktree dir in the background
```

- **`background_post_create`** — `claude --worktree` boots right away while hooks run detached. Progress and a terminal `running`/`ok`/`failed` status are written under `.nerve/hooks/<branch_slug>/`; `nerve list` and the TUI Worktrees tab show a `HOOKS` column so you can tell when bootstrap finished. Note the trade-off: an agent could start a dev server before `pnpm i` has finished — leave this off for projects where that bites.
- **`background_remove`** — teardown renames the worktree into `.nerve/trash/` (instant) and deletes the bytes in a detached process. git's own metadata is reconciled *synchronously* (`git worktree prune`), so its view never goes out of sync even if the background delete is interrupted; leftovers are swept on the next remove. Falls back to a synchronous delete if the worktree lives on a different filesystem than `.nerve/`. If a detached delete is ever interrupted, `nerve doctor` reports the leftover bytes and `nerve gc` clears them on demand.

### Install scope: which settings file

`.claude/settings.json` is the shared, team-committed Claude Code settings file. `.claude/settings.local.json` is the per-user override that Claude Code treats as gitignored. Because not everyone on your team will have nerve installed, by default `nerve hooks install --project` targets the **local** file so the hooks don't get committed:

| Flag | Target |
|---|---|
| `--project` (default) | `<repo>/.claude/settings.local.json` (user-local, gitignored — nerve also adds it to `.gitignore` on first install) |
| `--shared` | `<repo>/.claude/settings.json` (committed; use only if every collaborator has nerve) |
| `--user` | `~/.claude/settings.json` (every repo on your machine) |

When nerve creates a new worktree it auto-copies both `settings.json` and `settings.local.json` from the main checkout into the worktree's `.claude/`, so the hooks fire there too.

### Merge semantics for `nerve hooks install`

`nerve hooks install` merges into the target settings file — it never overwrites it. Each nerve-managed command string is tagged with the literal sentinel `# nerve-managed`, and `nerve hooks uninstall` removes only entries carrying that sentinel. Re-running install is idempotent: nerve strips its old sentinel entries first, then re-appends the current set. When you've defined your own hook for an event nerve also uses (e.g. `SessionStart`), both coexist — nerve appends alongside, it doesn't replace.

## Releasing

`goreleaser release --clean` builds darwin arm64+amd64 binaries and (once the tap stanza in `.goreleaser.yaml` is enabled) publishes to a Homebrew tap.
