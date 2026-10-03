# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Binny is a small, single-user file hosting app for personal use: upload files, keep them, get them
back. It is early — most of the app does not exist yet — so the sections marked _(open)_ record
decisions still being made. Update them as they settle rather than working around them.

## Git workflow

**Never commit unless asked.** Finish the work, leave it in the working tree, and report what
changed and how it was verified. The user reviews the diff themselves and then either asks for a
commit or makes it manually — that decision is theirs, not a step to anticipate. Don't stage files,
don't commit "so the work isn't lost", and don't treat a task being finished as permission.

When a commit *is* requested, only include files belonging to the task — leave unrelated
pre-existing changes and stray untracked files alone.

**One commit, one concern.** A commit has to be reviewable on its own and safe to revert on its own,
so split unrelated work rather than bundling it: a bug fix and a docs correction that happened to
land in the same session are two commits. Size is not the test — a change that genuinely touches
twenty files is still one commit if it is one concern. Say in the message what broke and why the fix
works, not just what you typed.

**Never push.** The user always pushes themselves. _(open: branches/PRs vs. committing straight to
`main`.)_

If an AGENTS.md is added, it is a short pointer telling other agents to read this file, not a copy
of it. This file is the single source of truth.

## Least code that does the job

Aim for the smallest diff that completes the task in full. This is meant to stay a small,
no-build, single-process app, and it stays approachable only while changes stay small.

- Prefer editing existing code over adding new code, and a few lines at the right call site over a
  new helper, module or class. Add a file only when something forces it.
- Reuse what is already here. A second implementation of something the repo already does is the
  most expensive kind of code.
- **No new dependency without asking first.**
- Leave out what nothing needs yet: config knobs, feature flags, an abstraction with one caller,
  `try`/`except` around code that doesn't raise, re-validation of data the route already validated.
- Deleting code is a legitimate way to finish a task. Say so when the fix turns out to be a removal.

This governs the amount of code, not the amount of work. Deliver everything that was asked and run
the verification the change calls for.

## Naming

A name is the cheapest documentation in the file. Make it a concise nameplate for what the thing is
or does — long enough to be unambiguous where it is *used*, short enough to read at a glance.

- Prefer the specific noun to the category: `stored_path`, `size_bytes`, `file_ids` over `value`,
  `data`, `items`.
- Name a function for what it returns or does, not how it works — `resolve_upload_path()`,
  `free_space()`, `formatSize()`.
- Don't encode the type or the scope in the name (`fileObjDict`, `tmpList`), and don't abbreviate
  past recognition.
- Follow the module you are editing — `snake_case` in Python, `camelCase` in JS — over any
  preference of your own.

Renaming existing code is its own task. Don't fold a rename into an unrelated change, where it
buries the real diff under noise.

## Run / test

_(open: stack not yet chosen. The default assumption is the same shape as Cozy — Flask, vanilla-JS
SPA with no build step, SQLite, `uv`, pytest, Docker — and the commands below follow it. Replace
them once the app exists.)_

```bash
uv run app.py --debug                 # dev server
BINNY_DATA_DIR=/path/to/data uv run app.py   # custom data directory (default: ./data)

uv run pytest                         # full suite (use `uv run`, not bare pytest)
uv run pytest tests/test_x.py::test_name -x

docker compose -f docker/compose.yml up --build
```

### How much verification a change needs

Run the tests that cover what was touched, and the full suite before anything the user is likely to
commit. When in doubt, run all of it.

**Booting the dev server is not required after every change.** Start it when the change can only be
confirmed in a browser — layout, upload progress, drag-and-drop, mobile behaviour, large-file
downloads — or when asked. For backend logic, docs and comments, skip it and say so. Never claim a
change was verified in the app when it wasn't.

## Architecture

_(open — fill in as modules land: a table of module → what it owns, plus the rules the code cannot
tell you on its own.)_

### Files on disk are the user's data

The stored files are the whole point of the app, so anything that writes, moves or deletes them is
held to a higher bar than the rest of the code:

- **Never trust a client-supplied filename or path.** Resolve every path under the data directory
  and refuse anything that escapes it (`..`, absolute paths, symlinks out). This needs a test.
- Write uploads to a temp file in the same filesystem and rename into place, so a dropped
  connection never leaves a half-written file under the real name.
- Stream uploads and downloads; never read a whole file into memory. Downloads should support
  range requests so large files and media resume and seek.
- Deleting a file is destructive and unrecoverable unless the app says otherwise. _(open: trash /
  soft delete?)_

_(open: storage layout — original names vs. content-addressed; whether a database indexes the
files or the filesystem is the index; folders vs. flat.)_

### Access

_(open: where it runs and who can reach it — LAN / VPN only, or exposed to the internet; login vs.
none; public share links and whether they expire.)_ Until this is decided, don't add an endpoint
that serves files without going through the same access check as the rest.
