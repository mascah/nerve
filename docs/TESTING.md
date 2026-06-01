# nerve — end-to-end test plan

A hands-on walkthrough of every nerve feature against a throwaway demo repo. Each section is independent enough to skip, but they're ordered so state from earlier sections feeds later ones.

> Time: ~15–20 minutes for the full pass.
> Tip: keep two terminals open — one in the demo repo, one in a worktree.

---

## 0. Prep

### Build the binary
```bash
cd /path/to/nerve                   # wherever you cloned it
make build
./bin/nerve version                 # → da4ab10-dirty or similar
```

(Or `make install` to drop it on `$GOPATH/bin`.)

### Pick a sandbox dir
Everything below runs against a throwaway repo so you can blow it away without touching your real projects:
```bash
SANDBOX=/tmp/nerve-walkthrough
rm -rf "$SANDBOX" && mkdir -p "$SANDBOX"
```

### Isolate the global registry (optional but recommended)
If you don't want this walkthrough to mutate your real `~/.config/nerve/projects.yaml`:
```bash
export XDG_CONFIG_HOME="$SANDBOX/.config"
```
Open every terminal you use during the walkthrough with this exported.

### Make the demo repo
```bash
mkdir -p "$SANDBOX/demo"
cd "$SANDBOX/demo"
git init -q -b main
git config user.email "you@example.com"
git config user.name "You"
echo "# demo" > README.md
echo "DEMO=1" > .env
echo "registry=https://example.com" > .npmrc
git add . && git commit -qm "init"
```

---

## 1. Project registry

### Register the demo repo
```bash
nerve project add "$SANDBOX/demo"
```
**Expected:** `registered demo -> /private/tmp/nerve-walkthrough/demo (lightweight; run \`nerve init\` inside the repo to configure)`

### List projects
```bash
nerve project list
```
**Expected:** one row, `demo  lightweight  -  /…/demo`

### Doctor (lightweight)
```bash
nerve doctor
```
**Expected:** `✓ git OK`, `• lightweight (no .nerve/config.yaml)`, `all checks passed`

---

## 2. Lightweight worktree

A project with no `.nerve/config.yaml` should still get a plain git worktree at the right path.

```bash
nerve new demo feat-light
```
**Expected output:**
```
worktree created
  branch:  feat-light
  path:    /…/demo/.worktrees/feat-light
```

**Verify:**
```bash
ls -la "$SANDBOX/demo/.worktrees/"           # → feat-light/ exists
cat "$SANDBOX/demo/.gitignore"               # → contains .worktrees/
git -C "$SANDBOX/demo" branch                # → feat-light listed
ls "$SANDBOX/demo/.worktrees/feat-light"     # → just the README (no .env / .npmrc cloned in lightweight mode)
```

---

## 3. Initialize a configured project

```bash
cd "$SANDBOX/demo"
nerve init
```
**Expected:** `wrote /…/.nerve/config.yaml`, then `appended 2 entries to .gitignore`, then a hint.

The freshly-written config has no services — that's intentional. Edit it now:

```bash
cat > .nerve/config.yaml <<'YAML'
version: 1
project:
    port_offset: 0
    worktree_root: .worktrees/{branch}
    pool_size: 10
services:
    - id: django
      base_port: 8000
      env_key: DOCKER_HOST_DJANGO_PORT
      primary: true
    - id: postgres
      base_port: 5432
      env_key: DOCKER_HOST_POSTGRES_PORT
    - id: vite
      base_port: 5173
      env_key: VITE_PORT
vars:
    - { env_key: WORKTREE_ID, value: "{{branch}}" }
clone_files:
    - { path: .env,    kind: file, required: true  }
    - { path: .npmrc,  kind: file, required: false }
templates:
    - { source: .env.example, dest: .env, merge: true }
hooks:
    pre_remove:
        - "echo 'pre-remove ran' > /tmp/nerve-walkthrough-preremove.txt"
YAML
```

### Re-run doctor — should now show "configured"
```bash
nerve doctor
```
**Expected:** `✓ config OK (3 services, 2 clone_files, 1 templates)` and `✓ port registry consistent (0 allocations)`.

---

## 4. Configured worktree (the main event)

```bash
cd "$SANDBOX/demo"
nerve -v new demo feat-configured
```

**Watch for, in order:**
1. `git worktree add ...`
2. `copying clone_files:` then `copied: .env` / `copied: .npmrc`
3. `wrote .../.env.local`
4. `running post_create hooks:` (or skipped if you used `--no-hooks`)
5. Summary table at the end with the offset and per-service ports.

**Verify the artifacts:**
```bash
cat "$SANDBOX/demo/.worktrees/feat-configured/.env.local"
# DOCKER_HOST_DJANGO_PORT=8001
# DOCKER_HOST_POSTGRES_PORT=5433
# VITE_PORT=5174
# WORKTREE_ID=feat-configured       (vars[] entry, value rendered through {{...}})

ls "$SANDBOX/demo/.worktrees/feat-configured/"
# .env  .npmrc  .env.local  README.md   (.env + .npmrc are the cloned files)

cat "$SANDBOX/demo/.nerve/ports.json"
# JSON with an allocation at "8001"
```

### Run it a second time to verify offset assignment
```bash
nerve -v new demo feat-second
cat "$SANDBOX/demo/.worktrees/feat-second/.env.local"
# DOCKER_HOST_DJANGO_PORT=8002, etc.
```

### Cross-project port collisions

Each project's `.nerve/ports.json` only knows about its own worktrees. To prevent two projects with overlapping port pools (e.g. project A at `base_port: 8000` and project B at `base_port: 8005`) from claiming the same TCP port, nerve also records every active per-service port in a user-wide leases file:

```
$XDG_CONFIG_HOME/nerve/ports.json     (default: ~/.config/nerve/ports.json)
```

This honors `XDG_CONFIG_HOME` the same way `projects.yaml` does. The allocator consults this file before picking an offset, and `nerve new` writes a lease for every per-service port it reserves. `nerve ports cleanup` (without `--project`) prunes orphan leases whose worktree no longer exists; `nerve doctor` flags them. You should never need to edit this file by hand.

---

## 5. Listing + inspection

```bash
nerve list                         # all projects' worktrees
nerve list demo                    # only demo's
nerve list --json                  # JSON output (good for scripts)

nerve ports list
nerve ports status                 # used / total / pool range
```

Inside a worktree, env discovery:
```bash
cd "$SANDBOX/demo/.worktrees/feat-configured"

nerve env                          # KEY=VAL lines
nerve env --shell                  # eval-friendly: `export K=V`
nerve env --json                   # JSON object

# Simulate what a Claude Code SessionStart hook does:
CLAUDE_ENV="$SANDBOX/claude.env"
CLAUDE_ENV_FILE="$CLAUDE_ENV" nerve env --inject
cat "$CLAUDE_ENV"                  # → port vars appended
```

From the main checkout, `nerve env` should be **silent** (no vars, exit 0):
```bash
cd "$SANDBOX/demo"
nerve env --json                   # → empty / no output
```

### Bootstrapping a new worktree (uv sync / pnpm i)

Don't try to copy `.venv` or `node_modules` from the main checkout. Python venvs bake absolute paths into `bin/python` shebangs and `activate`; `pnpm` uses hard-links into a content-addressed store and gets confused when the link target moves; `npm`/`yarn` with native deps (sharp, esbuild, lmdb) ship per-path platform metadata. On a warm package-manager cache the install commands take seconds — copying is not meaningfully faster and is meaningfully more fragile.

Instead, declare them as `post_create` hooks. They run **after** `clone_files` are copied and `templates` are rendered, so `.env` is on disk before install scripts read it:

```yaml
hooks:
    post_create:
        - "uv sync"
        - "pnpm install"
    pre_remove:
        - "echo 'pre-remove ran' > /tmp/nerve-walkthrough-preremove.txt"
```

### Backgrounded post_create + teardown (opt-in)

By default every `post_create` hook runs **foreground**: synchronously, in declared order, and `nerve new` blocks until they finish — so env-shaping hooks like `direnv allow` have taken effect before a session starts. Tag an individual hook with `background: true` to move that one command off the critical path. Background hooks run **detached and concurrently** with each other (and with `claude --worktree` booting):

```yaml
hooks:
    post_create:
        - direnv allow                                 # foreground: runs first, blocks boot
        - run: "sleep 5 && echo uv > uv.marker"        # background: detached
          background: true
        - run: "sleep 5 && echo pnpm > pnpm.marker"    # background: runs ‖ the one above
          background: true
project:
    background_remove: true
```

> The older project-wide `project.background_post_create: true` still works as a deprecated default (it backgrounds any hook that doesn't set its own `background:`), but prefer per-command control: leave prerequisites foreground and tag only the slow, independent installs.

Create runs the foreground hooks, then returns immediately and reports the background handoff:
```bash
nerve -v new demo feat-bg
# ...
# running post_create hooks:          # the foreground ones (e.g. direnv allow)
# post_create hooks running in background (see .nerve/hooks/feat_bg/log)
# Summary table prints right away — note branch_slug "feat_bg"

nerve list demo                    # HOOKS column shows "running", then "ok" after ~5s
cat "$SANDBOX/demo/.nerve/hooks/feat_bg/status.json"   # {"state":"running"...} → {"state":"ok"...}
cat "$SANDBOX/demo/.nerve/hooks/feat_bg/log"           # both background hooks' output
ls "$SANDBOX/demo/.worktrees/feat-bg/"*.marker         # uv.marker + pnpm.marker appear together (~5s, not ~10s)
```
Because the two background hooks run concurrently, both markers appear after ~5s, not ~10s. A failing background hook (`exit 1`) lands `{"state":"failed","exit_code":1,"failed_command":...}` and `nerve list` shows `failed`; the other background hooks still run to completion. A failing **foreground** hook instead aborts create and rolls the worktree back (no port leak), exactly as before.

Backgrounded remove returns instantly; git's view is reconciled synchronously so there's no phantom worktree:
```bash
nerve remove --force demo feat-bg
nerve list demo                    # feat-bg already gone (prune ran synchronously)
ls "$SANDBOX/demo/.nerve/trash/"   # the bytes may briefly remain here, deleted detached
```

`background_remove` falls back to a synchronous `git worktree remove` when `worktree_root` is on a different filesystem than `.nerve/` (the atomic rename into `.nerve/trash/` isn't possible across filesystems).

If a detached delete is ever interrupted, the bytes linger under `.nerve/trash/`. `nerve doctor` reports them as an informational `•` line (not an issue — they self-heal on the next remove), and `nerve gc [<project>]` clears them on demand:
```bash
nerve doctor                       # → "• N leftover item(s) in .nerve/trash (…) — run `nerve gc demo`"
nerve gc demo                      # → "cleared N item(s) (…) from .nerve/trash"
```

### Refresh after editing services, vars, or templates
1. Edit `.nerve/config.yaml`: add a new service (e.g. `redis`, base_port 6379, env_key `REDIS_PORT`), add a `vars` entry (e.g. `{ env_key: WORKTREE_TAG, value: "{{branch}}-{{ports.redis}}" }`), and change a `templates` source.
2. `cd` into a worktree and run `nerve refresh`.
3. Cat `.env.local`: the new service key appears with the correct offset, **and** the `vars` entry is (re-)rendered (`WORKTREE_TAG=feat-configured-<port>`). Any `templates` are re-rendered too.

`nerve refresh` writes the same artifacts `nerve new` does — per-service ports **+ static `vars` + `templates`** (both go through the shared `worktree.RenderEnv`, so the two paths can't drift). It deliberately does **not** re-copy `clone_files`, which are one-time copies made at create.

---

## 6. Removal

### Dirty check (should refuse)
The cloned `.env` + `.npmrc` are untracked in the worktree, so plain remove refuses:
```bash
nerve remove demo feat-second
# nerve: worktree has uncommitted changes — use --force to override
```

### Force remove
```bash
nerve remove --force demo feat-second
nerve ports list                   # 8002 released
```

### Remove the lightweight one
```bash
nerve remove --force demo feat-light
```

Keep `feat-configured` around for the TUI walkthrough below.

---

## 7. TUI walkthrough

Launch with no args:
```bash
nerve
```

You should land on the **projects list** with `demo` highlighted.

### Test transitions
| Press | Expected |
|---|---|
| `↓` / `↑` (or `j` / `k`) | cursor moves between projects |
| `a` | add-project form |
| `esc` (from add-project) | back to list |
| `⏎` on `demo` | project detail screen |
| `tab` (in project detail) | cycle Services → Clone Files → Templates |
| `q` | quit |

### Add a service via the form
1. From the projects list, `⏎` into `demo`.
2. Make sure you're on the **Services** tab.
3. Press `a`. The "add service" form opens.
4. Fill in:
   - id: `cache`
   - base_port: `11211`
   - env_key: `CACHE_PORT`
5. Tab to the primary toggle; leave unchecked (don't mark this primary — that'd reset the pool start).
6. Tab to Submit, press `⏎`.

You should bounce back to the project detail with `cache` now listed. Verify on disk:
```bash
grep -A1 "id: cache" "$SANDBOX/demo/.nerve/config.yaml"
```

### Delete a clone file via the TUI
1. From project detail, `tab` to **Clone Files**.
2. Use `↓` to pick a row, then `d`.
3. Row should disappear immediately. Verify on disk:
```bash
cat "$SANDBOX/demo/.nerve/config.yaml"
```

### Add a clone file
1. Still on Clone Files, press `a`.
2. Path: `.editorconfig`
3. Left/right arrows toggle kind (auto / file / directory).
4. Space toggles required.
5. Tab to Submit, `⏎`.

### Add a second project via the TUI
1. `esc` back to the projects list.
2. Press `a`.
3. Path: a different git repo (e.g. anything in `~/GitHub/`), or just leave the prefilled cwd.
4. Optional name.
5. `⏎` Submit.
6. New project appears in the list.

Quit with `q` and verify the registry file:
```bash
cat "$XDG_CONFIG_HOME/nerve/projects.yaml"
```

---

## 8. Claude Code hook integration

> Optional but most valuable — this validates the whole reason nerve exists.

### Install the hooks (project scope)
```bash
cd "$SANDBOX/demo"
nerve hooks install --project --dry-run     # preview the merged JSON
nerve hooks install --project               # actually write
cat .claude/settings.local.json             # verify
```

By default `--project` targets `.claude/settings.local.json` — the user-local override file, not the committed `.claude/settings.json` — so nerve hooks don't get shared with collaborators who may not have nerve installed. Nerve also adds `.claude/settings.local.json` to the repo's `.gitignore` on first install.

You should see 4 nerve entries: `WorktreeCreate`, `WorktreeRemove`, `SessionStart`, `CwdChanged`, each tagged with `# nerve-managed`. Use `--shared` to write to `.claude/settings.json` instead (only if every collaborator has nerve).

### Re-install is idempotent
```bash
nerve hooks install --project
diff <(nerve hooks install --project --dry-run) .claude/settings.local.json
# → no diff after the first install
```

### `claude --worktree` integration test

In `$SANDBOX/demo`:
```bash
claude --worktree feat-hooks
```

What should happen:
1. Claude Code fires the `WorktreeCreate` hook, which runs `nerve worktree-create`.
2. nerve creates `$SANDBOX/demo/.worktrees/feat-hooks/`, allocates a port, copies `.env`/`.npmrc`, writes `.env.local`.
3. nerve prints the worktree path on stdout; Claude opens its session in that dir.
4. SessionStart hook fires `nerve env --inject`, which appends port vars to `$CLAUDE_ENV_FILE`.
5. Inside the Claude session, ask it to run:
   ```
   env | grep DOCKER_HOST_DJANGO_PORT
   ```
   It should match the value in the worktree's `.env.local`.

### `cd` between worktrees mid-session
Create a second worktree first (`nerve new demo feat-other`), then in the same Claude session:
```
cd ../feat-other
env | grep DOCKER_HOST_DJANGO_PORT
```
The port should change — `CwdChanged` re-fires `nerve env --inject`.

### Uninstall

`nerve hooks install` **merges** into the target settings file — any hooks you defined yourself stay put, and nerve's entries are tagged with the literal sentinel `# nerve-managed` so uninstall can find and remove only them. If you've registered your own `SessionStart` hook, nerve appends alongside it rather than replacing it.

```bash
nerve hooks uninstall --project              # default target: settings.local.json
cat .claude/settings.local.json
# nerve hooks uninstall --shared             # if you ever installed with --shared
```
Only nerve-tagged entries should be gone; any other hooks in the file are preserved.

---

## 9. Cleanup

```bash
# Remove all the demo worktrees:
nerve remove --force demo feat-configured
nerve remove --force demo feat-hooks    # if you created it
nerve remove --force demo feat-other    # if you created it

# Deregister the project:
nerve project remove demo

# Blow away the sandbox:
rm -rf "$SANDBOX"
unset XDG_CONFIG_HOME
```

---

## Quick reference — exit codes

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | usage / generic error |
| 2 | git error |
| 3 | port pool exhausted |
| 4 | clone-files copy failed |
| 5 | worktree dirty (refused without `--force`) |
| 6 | unpushed commits (refused without `--force`) |
| 7 | not inside a worktree (when one was expected) |
| 8 | `nerve doctor` found issues |

---

## What to expect when something is wrong

- **`nerve new` fails with "port pool exhausted"** → `nerve ports cleanup` to drop stale allocations.
- **`nerve env --inject` is silent** → either cwd isn't in a registered project, project is lightweight, or the worktree isn't in `.nerve/ports.json`. Run `nerve env --json` to see whether anything is computed.
- **`claude --worktree` lands in `.claude/worktrees/...` instead of `.worktrees/...`** → hooks aren't installed (or aren't installed in the right scope). Run `nerve hooks install --project` from the repo root.
- **Env vars don't appear inside a Claude session running in a worktree** → make sure the worktree has its own `.claude/settings.local.json` (nerve auto-copies it on create; if you created a worktree before installing nerve hooks, manually copy it from the main checkout or recreate the worktree).
- **`nerve doctor` reports stale allocations** → run `nerve ports cleanup --project <name>`.
