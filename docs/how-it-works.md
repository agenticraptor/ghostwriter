# How ghostwriter works

ghostwriter is a thin, careful layer over `git`. It never reimplements diffing
or patching — it shells out to the `git` you already trust and parses its
output, preserving the exact patch bytes so any subset can be reconstructed and
reverted.

## The pipeline

```
                 ┌─ git diff HEAD ─────────────┐
working tree ────┤                             ├─► parse into files + hunks ─┐
                 └─ untracked new files ───────┘                             │
                                                                            ▼
                                              group into intents  ◄── LLM narrator
                                              (deterministic if no model)   │
                                                                            ▼
                                          annotate with risk flags + dep changes
                                                                            │
                  ┌──────────────────────────────┬──────────────────────────┘
                  ▼                               ▼
        interactive TUI                    render (term / md / json)
   accept / reject per intent                 (read-only)
                  │
                  ▼
   git apply --reverse (checked)  ──►  rejected intents reverted; rest kept
```

### 1. Collect (`internal/gitdiff`)

`git diff HEAD` captures every tracked change in the working tree (staged or
not). New, untracked files are listed with `git ls-files --others` and
synthesized into "added" diffs so the agent's brand-new files are part of the
review too. The parser keeps each file header and each hunk's **exact bytes**,
which is what makes safe reverting possible later.

### 2. Group into intents (`internal/intent`)

- **With a model:** the diff is rendered as an indexed listing (stable `F:H`
  ids) and sent to the narrator, which returns a small set of intents, each
  referencing the hunks that belong to it. Every hunk the model omits is
  swept into a final "Other changes" intent, so nothing is ever lost.
- **Without a model (`--no-ai` or no key):** changes are grouped
  one-intent-per-file with a category inferred from the path.

Crucially, **line counts and risk flags are always recomputed from the real
diff** — the model decides the *story*, never the *numbers*.

### 3. Annotate (`internal/risk`, `internal/deps`)

Each intent is tagged with deterministic risk flags (migrations, lockfiles,
dependency manifests, CI/Docker, infra, possible secrets, risky deletions) and
the concrete dependency additions/removals it contains.

### 4. Review

- **Interactive (default in a terminal):** a Bubble Tea TUI shows each intent as
  a card with its diff. You accept or reject with one key.
- **Non-interactive (`--print`, a pipe, or `-f markdown/json`):** the review is
  rendered read-only. It never modifies your files.

### 5. Apply rejections (`internal/gitdiff` again)

Before anything is changed, ghostwriter writes a backup of the rejected changes
to `<git-dir>/ghostwriter/rejected-<timestamp>.patch`, so you can always restore
reverted work with `git apply <that file>` — even if it was never committed.

Then, for each rejected intent, ghostwriter reconstructs a patch from exactly
the hunks involved and runs `git apply --reverse`:

- A **dry-run check** (`--check`) runs first. Only if it passes is the reverse
  patch actually applied. If it fails (e.g. interdependent hunks), that file is
  **reported** and left completely untouched — ghostwriter never half-applies a
  change or corrupts your tree. (Reverts are per-file, not transactional across
  files; the per-file ✓/✗ summary tells you exactly what changed.)
- A partial reject of a **renamed** file reverts only the selected content and
  keeps the rename, rather than undoing the move and stranding accepted hunks.
- Rejected **new** files are simply deleted (and the deletion refuses any path
  that would fall outside the repository).
- Accepted and undecided intents are left exactly as the agent left them.

### A note on safety

ghostwriter treats the repository contents as untrusted input. Untracked
symlinks are listed but never followed (so a crafted link can't leak files off
your machine), the `--against` ref is validated so it can't be smuggled in as a
git option, and secret-looking lines are redacted before any diff text reaches a
cloud model.

## Why read the working tree instead of hooking the agent?

Hooking a specific agent's edit stream is fragile and ties you to one tool. The
git working tree is the universal interface: **any** agent — Claude Code,
Cursor, Codex, Aider, or a human — leaves its changes there. Reading it means
ghostwriter works everywhere, today, with zero integration.
