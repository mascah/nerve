# Known limitation: `EnterWorktree` does not load worktree env mid-session

**Status:** open, tracked here. Upstream (Claude Code) fix not yet filed.
**Observed on:** Claude Code 2.1.149, direnv 2.37.1, nerve 66367a1 (macOS).
**Affects:** the `nerve env --inject` (`SessionStart`/`CwdChanged`) integration, and equally any cwd-driven env tool (e.g. the `claude-code-direnv` plugin).

## Summary

`claude --worktree <name>` works: the session starts with cwd already in the worktree,
`SessionStart` fires, and `nerve env --inject` (+ direnv) populate the session env.

The **`EnterWorktree` tool** (Claude creating/entering a worktree *mid-session*) does **not**
load the worktree env. After it runs, Bash-tool commands execute in the worktree directory but
still see the **main checkout's** env — wrong `DATABASE_URL`, ports, `VIRTUAL_ENV`, `PATH`.

This is a Claude Code platform gap, not a nerve or direnv bug. Both tools are correctly built
around the events that *can* inject env; `EnterWorktree` simply doesn't emit any of them.

## How env reaches Bash-tool commands

The only channel is **`$CLAUDE_ENV_FILE`**: a hook appends `export KEY=VALUE` lines to it, and
Claude Code applies that content as a preamble before each Bash-tool command. Neither nerve nor
direnv changes Claude's own process env. Both compute *what* to inject from the **cwd of the hook
subprocess** (`nerve env --inject` calls `os.Getwd()`; direnv uses `pwd -P` → `direnv export`).

`CLAUDE_ENV_FILE` is set for only four hook events: **`SessionStart`, `Setup`, `CwdChanged`,
`FileChanged`** (per the Claude Code hooks docs). Any other event's hook cannot inject env.

## Root cause

`EnterWorktree` switches the session cwd into the new worktree but fires **only `WorktreeCreate`** —
never `CwdChanged` or `SessionStart`. Verified via a hook logger:

```
PreToolUse EnterWorktree → WorktreeCreate → PostToolUse EnterWorktree → (next Bash)
# 0× CwdChanged, 0× SessionStart, 1× WorktreeCreate
```

So:

- The events that *can* write `CLAUDE_ENV_FILE` (`CwdChanged`/`SessionStart`) never fire on
  worktree entry, so nothing re-injects. `CLAUDE_ENV_FILE` keeps only what `SessionStart` wrote at
  launch — the **main checkout's** env.
- `WorktreeCreate` does fire, but (a) it does **not** receive `CLAUDE_ENV_FILE`, and (b) it runs with
  cwd = **main checkout** (the worktree is being created *by* this hook; the session hasn't moved in
  yet), so even a cwd-based injector would evaluate the wrong directory. Confirmed: from the main
  checkout `nerve env --inject` is a no-op ("cwd is the main checkout") and `direnv export bash`
  yields the main `DATABASE_URL`.

By contrast, a plain `cd` between Bash commands *does* fire `CwdChanged` — that's the basis of the
workaround below.

## Approaches that do NOT work (tested)

- **Register a hook on `WorktreeCreate`** (e.g. point direnv/`nerve env --inject` at it): no
  `CLAUDE_ENV_FILE` on that event, and wrong cwd. Also dangerous — `WorktreeCreate`'s **stdout is the
  worktree path** Claude consumes (that's how `nerve worktree-create` returns it), so a hook that
  prints exports to stdout there can corrupt the path.
- **`PostToolUse` matched to `EnterWorktree`**: same flaw — `PostToolUse` does not receive
  `CLAUDE_ENV_FILE`, so it cannot inject env. (It *can* return `additionalContext` to nudge the agent.)
- **Writing the session env file directly from `WorktreeCreate`**: the files live at
  `~/.claude/session-env/<CLAUDE_CODE_SESSION_ID>/<event>-hook-<N>.sh`. Tested three ways in a live
  session — (1) a new out-of-band file, (2) appending to a tracked file, (3) appending to the
  *currently-applied* file — **none** were picked up. Claude **snapshots each env file's contents when
  its producing hook exits** and re-applies that snapshot; it does not re-read the files from disk or
  glob the directory. The channel is open only to hooks Claude itself invokes with `CLAUDE_ENV_FILE`.
  (Note: the var is `CLAUDE_CODE_SESSION_ID`, not `CLAUDE_SESSION_ID`/`CLAUDE_HOME`.)

## Workaround: `nerve bash-preamble` (`PreToolUse:Bash` command rewrite)

A `PreToolUse` hook matched to `Bash` rewrites the pending command via the documented
`hookSpecificOutput.updatedInput.command`, prepending a per-command env load. Because every Bash
command then self-loads the env from its own cwd, commands run after `EnterWorktree` (cwd = worktree)
are correct — no `CwdChanged`, no `CLAUDE_ENV_FILE`, no agent cooperation required.

This is implemented as `nerve bash-preamble` and installed **opt-in**:

```bash
nerve hooks install --bash-preamble
```

which adds:

```jsonc
"PreToolUse": [
  { "matcher": "Bash",
    "hooks": [{ "type": "command", "command": "nerve bash-preamble # nerve-managed" }] }
]
```

How it works: the command reads the hook stdin JSON, takes `.cwd` and `.tool_input.command`; if
`.cwd` is a registered + configured nerve worktree with an allocation it emits JSON setting
`updatedInput.command` to `<env-load>\n<original command>`. Outside a registered worktree it prints
nothing and exits 0, so main-checkout and unregistered-repo commands are untouched. It deliberately
emits **only** `updatedInput` (no `permissionDecision`) — this avoids both the historical
"`updatedInput` dropped when `permissionDecision: allow`" bug and auto-approving every Bash command.

The `<env-load>` is governed by the per-project `project.bash_preamble` field in `.nerve/config.yaml`:

- **Unset (default):** nerve prepends its own computed `export KEY=VALUE` port lines (the same vars
  as `nerve env --shell`), evaluated in-process — no nested `nerve`/`git` subprocess per command.
- **Set:** nerve prepends the value verbatim. Use `eval "$(direnv export bash 2>/dev/null)"` to
  delegate the load to direnv, which (if your `.envrc` does `dotenv_if_exists .env.local`) re-exports
  the nerve ports **and** composed vars like `DATABASE_URL`/`VIRTUAL_ENV` in one shot:

  ```yaml
  project:
    bash_preamble: eval "$(direnv export bash 2>/dev/null)"
  ```

Trade-offs: runs on every Bash command (direnv is mtime-cached, so cheap when unchanged); **don't
stack multiple Bash-rewriting `PreToolUse` hooks** — Claude Code honors only one `updatedInput`
("last to finish wins"), so if another tool (e.g. a direnv plugin) also rewrites Bash, one of them is
silently dropped. That's why this hook is opt-in: pick exactly one Bash rewriter. The rewritten
command is what executes.

> Manual stopgap (no hook needed): after `EnterWorktree`, `cd` into a **subdirectory** of the
> worktree (a round-trip back to the worktree root is a net-zero cwd change and fires nothing). That
> triggers `CwdChanged`, which runs the existing `nerve env --inject` + direnv hooks with the correct
> cwd.

## Proper fix (upstream, to file)

`EnterWorktree` should emit `CwdChanged` (or otherwise re-run the `CLAUDE_ENV_FILE` hooks) after
switching cwd into the worktree. Then the existing `CwdChanged` → `nerve env --inject` hook works
with zero changes. Repro for the report:

1. nerve-registered repo with services; `nerve hooks install`.
2. Start a normal `claude` session in the main checkout.
3. Have Claude call `EnterWorktree` for a new branch.
4. Run a Bash command echoing a composed var (e.g. `$DATABASE_URL`) — it shows the **main checkout's**
   value, and only `WorktreeCreate` fired (no `CwdChanged`/`SessionStart`).

Expected: worktree entry fires `CwdChanged` so cwd-sensitive env hooks re-run.
