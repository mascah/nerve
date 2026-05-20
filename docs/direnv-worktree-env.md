# Stale session env in `claude --worktree` (the direnv problem)

> Status: **problem statement / design note**, not yet implemented.
> Tracks GitHub issue #5. The likely home for the fix is a **separate Claude
> Code plugin** rather than nerve core — see [Design space](#design-space).

## Problem

When Claude Code starts a session via `claude --worktree <name>`, nerve's
`WorktreeCreate` hook (`internal/cli/worktree_create.go`) creates the worktree
and Claude starts its session with `cwd = <worktree-path>`. But **Claude is not
a shell** — `direnv` never fires against the worktree's `.envrc`. The session's
process environment is inherited verbatim from the shell that ran `claude`,
which is typically the main checkout (where direnv has already loaded the *main*
`.envrc`).

Nerve's `SessionStart` hook (`nerve env --inject`) then writes per-service port
keys (`DOCKER_HOST_POSTGRES_PORT=5435`, etc.) into `$CLAUDE_ENV_FILE`, and
Claude merges those over the inherited env.

The result is a session env that is **partly correct, partly stale, with no
obvious indication of which is which.**

## Concrete trace

Starting state: shell in main checkout with direnv loaded. Run
`claude --worktree feat-foo`. Resulting session env:

| Variable | Value | Source | Correct for worktree? |
|---|---|---|---|
| `cwd` | `.worktrees/feat-foo/` | Claude `--worktree` | yes |
| `DOCKER_HOST_POSTGRES_PORT` | `5435` | nerve `SessionStart` injection | yes |
| `DOCKER_HOST_DJANGO_PORT` | `8003` | nerve `SessionStart` injection | yes |
| `VIRTUAL_ENV` | `.../demo-server/.venv` | Inherited from parent shell | no (main's venv) |
| `PATH` | `.../demo-server/.venv/bin:...` | Inherited from parent shell | no (main's bin) |
| `DATABASE_URL` | `postgres://...@localhost:5432/demo` | Inherited from parent shell | no (main's port 5432, not worktree's 5435) |
| `OPS_DATABASE_URL` | `...@localhost:5450/...` | Inherited from parent shell | no |
| `REDIS_URL` | `...localhost:6379...` | Inherited from parent shell | no |
| `WORKTREE_ID` | `main` (or unset) | Inherited from parent shell | no |

The most dangerous case is `DATABASE_URL`: any host-side command in the session
— `uv run pytest`, `python manage.py migrate`, `dbshell`, `psql $DATABASE_URL` —
runs against the **main checkout's** database, not the worktree's. pytest
fixtures hitting the wrong DB is silent data loss.

## Why nerve's port injection isn't enough

Static values like `DOCKER_HOST_POSTGRES_PORT` get overridden cleanly by nerve's
injection. **Composed** values like `DATABASE_URL` do not — they were composed
*in the parent shell* using the parent's port (5432), and that string is now
baked into the env. Nerve injecting an updated `DOCKER_HOST_POSTGRES_PORT=5435`
afterwards doesn't retroactively re-expand `DATABASE_URL`.

The same shape applies to `VIRTUAL_ENV` / `PATH` (set by `.venv/bin/activate`
sourced from `.envrc`) and any other shell-evaluated logic in the project's
`.envrc`.

This is the dividing line that motivates the rest of nerve's env story:

- **Static, port-derived values** → nerve injects them today (`nerve env --inject`).
- **Static, string-valued, file-only values** (e.g. `WORKTREE_ID` for
  `docker compose --env-file`) → handled by the `vars:` block (issue #6).
- **Composed / shell-evaluated values** (`DATABASE_URL`, `PATH`, `VIRTUAL_ENV`)
  → this document. They are exactly what `.envrc` already computes for the
  manual-`cd` flow; the gap is only that `claude --worktree` never triggers it.

## The manual workaround

Open a new terminal, `cd .worktrees/feat-foo` (direnv evaluates the worktree's
`.envrc`), then `claude` from there. This is the existing flow that `--worktree`
was meant to replace.

## Design space

The *problem* (stale composed session env) is generic to any multi-service
worktree. The natural *solution* (evaluate `.envrc` and merge the delta) is
specific to direnv users. That asymmetry is the central design tension: the
value of any fix concentrates almost entirely in projects shaped like this one
(direnv + docker + host-run commands). So the goal is a fix that costs nothing
for everyone else and does not drag a direnv dependency into nerve's core.

### Option A — Separate Claude Code plugin (leading candidate)

Ship a small, standalone Claude Code plugin that installs its **own**
`SessionStart` + `CwdChanged` hook. The hook runs `direnv export json` with
`cwd = <worktree>` and merges the resulting env-delta into `$CLAUDE_ENV_FILE`.

- **Pros:** keeps direnv entirely out of nerve; opt-in by installation; usable
  even by people who don't use nerve at all (any `claude --worktree` +
  direnv user benefits); independent release cadence.
- **Cons:** a second thing to install; needs to coexist cleanly with nerve's own
  `SessionStart` hook (both append to `$CLAUDE_ENV_FILE` — last write wins, so
  ordering matters; the direnv hook should run so nerve's authoritative port
  vars are not clobbered, or vice-versa by design).

### Option B — First-class nerve feature

Extend `nerve env --inject` to run `direnv export` when `direnv` is on `PATH`
and `<worktree>/.envrc` exists, merging the delta alongside the port keys.

- **Pros:** one mechanism; "whatever `cd && direnv` does, `claude --worktree`
  does."
- **Cons:** couples nerve to direnv (external binary, `.envrc`-execution
  surface); benefits only direnv projects while every nerve user carries the
  code. Reserve for after a second data point or an explicit decision to bless
  direnv as a dependency.

### Option C — Documented hook recipe

Document a `SessionStart` hook the user adds themselves (nerve already installs
alongside additional hooks for the same event). Roughly:

```sh
# .claude/settings(.local).json SessionStart hook
if command -v direnv >/dev/null && [ -f "$PWD/.envrc" ]; then
  direnv export json | nerve-or-jq-merge-into "$CLAUDE_ENV_FILE"
fi
```

- **Pros:** zero new code; ships as documentation today.
- **Cons:** every project re-implements the parsing + merge; `unset` and
  `direnv allow` handling are fiddly to get right by hand.

### Option D — Pluggable session-env provider (generic, future)

If demand generalizes beyond direnv, add a config knob naming a command nerve
runs at `SessionStart` with `cwd = <worktree>` (and the correct ports already in
its env), merging stdout. `direnv export json` is then just one provider;
`mise`, a Makefile target, or a custom script are others. This is the
de-overfit version of Option B — build it only once a second tool actually wants
it.

## Recommendation

Start with **Option A (separate plugin)**. It solves the real, dangerous problem
for the projects that have it, keeps nerve's core tool-agnostic, and is useful
independent of nerve. Fall back to **Option C** as the stopgap if the plugin
isn't ready. Treat **B/D** as later moves gated on a second data point.

## Implementation notes (for whichever option)

- **Interface:** prefer `direnv export json` — a clean JSON object mapping
  changed keys to new values, with `null` meaning *unset*. Easier to parse than
  `direnv export bash`.
- **Correct ports during evaluation:** run the export with the worktree's
  correct port vars already in the subprocess env (or rely on the worktree's
  `.envrc` sourcing the nerve-written `.env.local`) so composed values like
  `DATABASE_URL` interpolate the *worktree's* port, not the parent's.
- **Authoritative port keys win:** if both nerve and direnv produce a key, the
  nerve-computed port value should win on collision.
- **Testable seam:** make the `direnv export` call injectable (a function
  variable / small interface) the way `internal/ports` injects `ProbeFunc`, so
  unit tests can feed a canned JSON delta with no real direnv on the box.
- **Graceful degradation:** direnv missing / `.envrc` absent / `.envrc` blocked
  → fall through to port-only injection. Surface the reason only under a verbose
  flag (matching `nerve env --inject --verbose`); do not noise up every session.

## Open questions

- **Does `$CLAUDE_ENV_FILE` support `unset`?** The file is *sourced* as a shell
  preamble (it already uses `export` lines), so `unset KEY` lines should work —
  needs empirical confirmation. If it is set-only, pure unsets (vars present in
  main but absent in the worktree) can't be cleared; the value-differs cases
  (the dangerous ones: `DATABASE_URL`, `PATH`, `VIRTUAL_ENV`) are still fixed by
  appending the worktree value as an override.
- **`direnv allow`:** a freshly-created worktree's `.envrc` is blocked until
  allowed; `direnv export` on a blocked dir yields nothing. Either auto-allow at
  worktree-create (the content is the user's own repo) or document that the
  project's `post_create` hook runs `direnv allow`.
- **cd-between-worktrees:** `CwdChanged` fires on each `cd`. With an append-only
  `$CLAUDE_ENV_FILE`, later-sourced lines override earlier ones, so transitions
  should be correct but the file grows. Confirm transition behavior against a
  live `claude --worktree` session.

## References

- GitHub issue #5
- `internal/cli/env.go`, `internal/envinject/envinject.go` — current
  `nerve env --inject` path
- `internal/envfile/inject.go` — `$CLAUDE_ENV_FILE` append logic
- `internal/hooks/hooks.go` — `SessionStart` / `CwdChanged` hook installation
- Companion: `vars:` block (issue #6) for static string values in `.env.local`
