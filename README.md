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
| `nerve hooks install` | Wire into Claude Code |
| `nerve worktree-create` / `nerve worktree-remove --from-hook` | Stdin-driven entry points for Claude Code hooks |
| `nerve refresh` | Re-render templates + env in cwd worktree |
| `nerve doctor` | Diagnose config + registry |

## Claude Code integration

`nerve hooks install --project` writes the following into `<repo>/.claude/settings.json`:

| Hook event | Command | Purpose |
|---|---|---|
| `WorktreeCreate` | `nerve worktree-create` | Replaces Claude's default git logic; nerve creates the worktree, allocates ports, copies clone files, and prints the path back to Claude Code |
| `WorktreeRemove` | `nerve worktree-remove --from-hook` | Runs pre-remove hooks, releases the port, deletes the worktree + branch |
| `SessionStart` + `CwdChanged` | `nerve env --inject` | Appends per-worktree port env vars to `$CLAUDE_ENV_FILE` so Bash tool calls see them |

After install, `claude --worktree feat-foo` from a nerve-registered repo will create the worktree at `<repo>/.worktrees/feat-foo/` (instead of Claude's default `<repo>/.claude/worktrees/`), with ports allocated and env vars wired up for the session.

### Merge semantics for `nerve hooks install`

`nerve hooks install` merges into an existing `.claude/settings.json` — it never overwrites it. Each nerve-managed command string is tagged with the literal sentinel `# nerve-managed`, and `nerve hooks uninstall` removes only entries carrying that sentinel. Re-running install is idempotent: nerve strips its old sentinel entries first, then re-appends the current set. When you've defined your own hook for an event nerve also uses (e.g. `SessionStart`), both coexist — nerve appends alongside, it doesn't replace.

## Releasing

`goreleaser release --clean` builds darwin arm64+amd64 binaries and (once the tap stanza in `.goreleaser.yaml` is enabled) publishes to a Homebrew tap.
