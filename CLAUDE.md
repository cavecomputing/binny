# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Binny is a small, single-user file hosting app for personal use: a file explorer in the browser with
folders, uploads, downloads, moves, renames, tags and tag search, and a trash. It runs on the LAN or
over Tailscale behind Caddy and is never exposed to the internet. Its look and its tag handling
follow [imgy](https://github.com/cavecomputing/imgy).

The app is early — most of it does not exist yet — so sections marked _(open)_ record decisions
still being made. Update them as they settle rather than working around them.

## What it does, and what it deliberately doesn't

- **Explorer:** nested folders; create, rename, move (drag-and-drop and a "move to" picker),
  upload (button and drag-and-drop, many files at once), download (single files; folders and
  multi-selections as a zip).
- **Tags:** any file or folder can carry tags, and the search bar filters by them. Reuse imgy's
  ideas — a small expression syntax (`tag`, `-tag`, `+tag`, `old>new`), tab completion, bulk tagging
  of a selection — rather than inventing new ones.
- **Trash:** deleting moves an item to the trash, where it can be restored. Nothing is removed for
  good until the trash is **emptied by hand**; no automatic expiry.
- **Login:** one password, no usernames. A successful login sets a long-lived cookie so each device
  only logs in once.
- **No sharing.** No public links, no second user, no permissions model. Don't add one.

## Git workflow

Commit as the work goes and **push straight to `main` without asking**: this project uses no
feature branches or PRs. Only include files belonging to the task, and leave unrelated pre-existing
changes and stray untracked files alone. Push once a piece of work is done and verified, not
half-finished.

**One commit, one concern.** A commit has to be reviewable on its own and safe to revert on its own,
so split unrelated work rather than bundling it: a bug fix and a docs correction that happened to
land in the same session are two commits. Size is not the test — a change that genuinely touches
twenty files is still one commit if it is one concern. Say in the message what broke and why the fix
works, not just what you typed.

This file is the only agent doc. Don't add an `AGENTS.md` or a second copy of these rules.

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

Flask backend, vanilla-JS frontend with no build step, same shape as cozy and imgy: SQLite, `uv`,
pytest, Docker. _(open: the commands below are placeholders until the app exists.)_

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
- Delete means **move to trash**. Only "Empty trash" (and deleting a single item from inside the
  trash) removes a file for good, and both ask for confirmation in the UI.

_(open: storage layout. The likely answer, following imgy, is that real folders on disk are the
source of truth and SQLite only holds metadata — tags, trash records, settings — keyed by path, so
a move or rename must update those rows in the same request.)_

### Access

Binny sits on the LAN or Tailscale behind a Caddy reverse proxy, never on the public internet, but
it still has a login because anything on the tailnet can reach it.

- **Every route except the login page and its static assets requires the session cookie**,
  including file downloads and thumbnails. A new route is behind the check by default, not opted in.
- The password comes from the environment (or a hash of it in the data directory), never from a
  file in the repo. Compare it in constant time.
- The cookie is `HttpOnly`, `SameSite=Lax`, and long-lived (months), so a device stays logged in.
  _(open: how to log every device out — rotating the secret key is the simplest answer.)_
- Behind Caddy, honour `X-Forwarded-Proto` only from the proxy so `Secure` cookies and redirects
  are right; Caddy passes the Host header through unchanged by default.
