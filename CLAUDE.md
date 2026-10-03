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
- **Downloader:** paste a URL and pick a destination folder; the server fetches the file and saves
  it there, with progress shown in the UI. The download runs server-side, so it keeps going when the
  browser tab closes. _(open: plain HTTP(S) only, or also yt-dlp-style site support? A queue with
  several downloads at once?)_
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
pytest, Docker. _(open: the app and test commands are placeholders until the app exists.)_

```bash
uv sync                               # install the locked dependencies into .venv
uv run app.py --debug                 # dev server
BINNY_PASSWORD=changeme uv run app.py       # the password is required
BINNY_DATA_DIR=/path/to/data uv run app.py   # custom data directory (default: ./data)

uv run pytest                         # full suite (use `uv run`, not bare pytest)
uv run pytest tests/test_x.py::test_name -x

docker compose -f docker/compose.yml up --build
```

Dependencies are declared in `pyproject.toml` and pinned in `uv.lock`; Python is pinned in
`.python-version`. Change them with `uv add` / `uv remove` (or edit `pyproject.toml` and run
`uv lock`) and commit both files together. Never `pip install` into the project, and never bump the
`0.0.0` in `pyproject.toml` — it is a packaging placeholder, not a version.

### How much verification a change needs

Run the tests that cover what was touched, and the full suite before anything the user is likely to
commit. When in doubt, run all of it.

**Booting the dev server is not required after every change.** Start it when the change can only be
confirmed in a browser — layout, upload progress, drag-and-drop, mobile behaviour, large-file
downloads — or when asked. For backend logic, docs and comments, skip it and say so. Never claim a
change was verified in the app when it wasn't.

## Architecture

Single-process Flask app, vanilla-JS frontend, no build step. The layout is meant to be readable at
a glance: one thing per file, named for what it owns, so finding the code for a feature never takes
a search. Keep it that way — a new feature gets its own blueprint or module rather than growing a
neighbour.

| Path | Owns |
|---|---|
| `app.py` | Entry point, and the **only** Python file at the repo root: `app = create_app()`, which `uv run app.py` and `gunicorn app:app` both name. No logic lives here. |
| `binny/` | The application package. Everything else in Python goes in here. |
| `binny/__init__.py` | `create_app()`: config, the login guard, `init_db()`, blueprint registration. |
| `binny/config.py` | Paths and limits, read once from the environment (`BINNY_DATA_DIR`, `BINNY_PASSWORD`). |
| `binny/db.py` | Schema (`init_db()`, idempotent) and `get_db()`. |
| `binny/storage.py` | The one place that turns a user-supplied path into a real one (`safe_path()`), plus file walking. |
| `binny/api/` | One Flask blueprint per resource — `files`, `folders`, `tags`, `trash`, `downloads`, `auth` — all under `/api`. |
| `binny/templates/index.html` | The page shell. |
| `binny/static/js/` | ES modules, one per concern (`api.js`, `state.js`, `explorer.js`, `tags.js`, `trash.js`, `downloads.js`, …), entry `main.js` loaded with `<script type="module">`. |
| `binny/static/css/` | `cavecomputing.css` (the vendored design system, copied unchanged from imgy) and `style.css` (Binny's layout and Gruvbox tokens). |
| `tests/` | pytest, one file per blueprint or module. |
| `docker/` | Dockerfile and `compose.yml`, as in imgy. |

Fill in the "Owns" column with real names as modules land, and add the rules the code can't tell
you on its own under it.

### Frontend conventions

- Native ES modules only, `import`/`export` with relative paths. No bundler, no framework, no
  globals except what `main.js` deliberately wires up.
- Call the server through `api.js`, which throws on errors and shows them to the user.
- Modules that bind listeners export an `initX()`; `main.js` calls them in order.
- The look is the **cavecomputing design system**
  ([reference](https://claude.ai/artifact/TAYcpHgxU55sLKKU2sYeRv): read its `project/README.md`,
  `project/tokens.json` and `project/components/bundle.css`). Copy its tokens and its `bundle.css`
  unchanged into `binny/static/css/` and build on the `cc-*` components (`cc-shell`, `cc-btn`,
  `cc-tag`, `cc-badge`, `cc-callout`, `cc-card-header`, `cc-input`, …) before writing new ones.
  Change the design system, not the copy.
- Colors come from its tokens; never hardcode them. Accents keep one meaning: yellow
  (`accent-text`) for focus, selection and active rows; green (`accent-alt`) for the brand and
  section icons; aqua for done; blue (`accent-cool`) for paths and links; orange (`accent-warm`) for
  anything destructive. There is no red. No web fonts: system mono and sans only.
- **Dark is the default theme**, whatever the OS prefers. The choice lives in `localStorage`
  (`binny-theme`), so each device keeps its own; never sync it through the server. Apply it in a
  tiny inline script in `<head>` before the stylesheet paints, so a light-theme device doesn't
  flash dark on load. Wrap storage access in `try` so a blocked `localStorage` still gets dark.
- **Logo and favicon match the sibling apps** (cozy, imgy, campfire) so they sit together in a tab
  bar: a `#282828` rounded square with a 24px stroked line icon in one Gruvbox accent, 2.2 stroke,
  round caps. Binny's is a folder in aqua (`#8ec07c`) — cozy is yellow, imgy green, campfire
  orange. [binny/static/favicon.svg](binny/static/favicon.svg) is the source and `favicon.png`
  (256px) is rendered from it; change both together. The top-left brand is the same folder icon
  in `accent-aqua` beside the `cc-wordmark`.
- The [interface mockup](https://claude.ai/artifact/7KsCx3jSnVbB6tydGm55db) is the reference for layout: the "+" split button (click uploads files;
  the arrow opens "Upload from link"), folder and tag sidebar, file table with tag chips, trash
  view, downloads panel, sign-in card.

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

### Data lives in two places

```
data/                 # BINNY_DATA_DIR, default ./data
├── files/            # the user's files, as ordinary files and folders
│   └── .trash/       # trashed items, hidden
└── binny.db          # SQLite: tags, trash records, settings
```

- **`data/files/` is the source of truth.** It is a plain folder tree the user can open in any file
  manager, so the app's folders are real folders and a file's name on disk is its name in the app.
  Never rename, hash or wrap files for the app's convenience. Files added, moved or deleted outside
  the app show up on the next listing; the app must cope with that rather than assume it owns the
  tree.
- **`data/binny.db` holds only metadata, never file contents**, keyed by the path relative to
  `data/files/`. It exists so listing, sorting and tag search never have to walk the disk:
  - per item: path, folder or file, size in bytes, modified time (`mtime`), and its kind (image,
    video, document, archive…) from the extension;
  - tags, in their own table joined to paths, so a tag search is one indexed query;
  - per folder: item count and total size, so the explorer can show them without recursing.

  Treat it as a cache of the disk plus the things only it knows (tags, trash records). Size, mtime
  and kind can always be rebuilt from `data/files/`; compare `mtime` and size against the disk to
  notice outside changes. Tags and trash records cannot be rebuilt, so never drop them in a resync.
- Because rows are keyed by path, anything the database knows about a file must survive the file
  disappearing or appearing from outside the app. A move or rename through the app updates its rows in the same request; a row whose file is
  gone is stale, not an error.
- **Trash is `data/files/.trash/`.** The app hides it (and every dot-folder) from listings, tag
  search and folder totals, and never lets an upload, move, rename or download target it except
  through the trash routes. Trashed items keep their tags and remember their original path so
  restore can put them back.

### The downloader fetches URLs on the server's behalf

That makes it the one place where Binny reaches out to the network, so:

- Stream the response to a temp file in the destination's filesystem and rename into place, the
  same as an upload. Never buffer it in memory.
- Take the filename from `Content-Disposition` or the URL, then run it through the same
  path-safety check as an upload; never let the remote server choose the path.
- Cap the size and set connect/read timeouts so one bad URL can't fill the disk or hang a worker.
- Run it off the request thread (it can take minutes), and keep its state where the UI can poll it.

### Access

Binny sits on the LAN or Tailscale behind a Caddy reverse proxy, never on the public internet, but
it still has a login because anything on the tailnet can reach it.

- **Every route except the login page and its static assets requires the session cookie**,
  including file downloads and thumbnails. A new route is behind the check by default, not opted in.
- The password is the `BINNY_PASSWORD` environment variable, so a Docker container sets it in
  `compose.yml` or an `.env` file. Never store it in the repo, the database or the data directory.
  Refuse to start when it is unset or empty rather than running without a login. Compare it in
  constant time.
- The cookie is `HttpOnly`, `SameSite=Lax`, and long-lived (months), so a device stays logged in.
  _(open: how to log every device out — rotating the secret key is the simplest answer.)_
- Behind Caddy, honour `X-Forwarded-Proto` only from the proxy so `Secure` cookies and redirects
  are right; Caddy passes the Host header through unchanged by default.
