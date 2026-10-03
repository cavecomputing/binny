# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Binny is a small, single-user file hosting app for personal use: a file explorer in the browser with
folders, uploads, downloads, moves, renames, tags and tag search, and a trash. It runs on the LAN or
over Tailscale behind Caddy and is never exposed to the internet. Its look and its tag handling
follow [imgy](https://github.com/cavecomputing/imgy).

The app is early — most of it does not exist yet — so sections marked _(open)_ record decisions
still being made. Update them as they settle rather than working around them.

## What it does, and what it deliberately doesn't

- **Navigation has three parts, each with one job:** the sidebar folder tree for moving sideways
  (it opens only the branch you are in; other branches open by their arrow), the breadcrumb for
  the exact path and moving up, and the file list for moving down. The sidebar collapses (button
  or `[`, remembered per device) so breadcrumb and list can stand alone; on phones it is a drawer,
  which also holds the theme switch and sign-out, as the top bar has no room for them there.
- **Explorer:** nested folders; create, rename, move (drag-and-drop and a "move to" picker),
  upload (button and drag-and-drop, many files at once), download (single files; folders and
  multi-selections as a zip).
- **Tags:** any file or folder can carry tags, and the search box finds things anywhere by tag or
  name. It reuses imgy's ideas rather than inventing new ones: the expression syntax below, tag
  suggestions with counts, tab completion, and one tag editor for an item or a whole selection that
  previews what Enter will do. Tags are lowercase, at most 100 characters, with no spaces, commas
  or `>`, and can't start with `-`, `+` or `@`.

  | Typed | In the search box | In the tag editor (T) |
  |---|---|---|
  | `tag` | items that have it | adds a tag already in use |
  | `-tag` | items that don't | takes it off |
  | `+tag` | same as `tag` | adds a new tag |
  | `@text` | names with the text in them, to the end of the line; case, and `_`, `-` and spaces, don't matter | — |
  | `old>new` | renames a tag everywhere, asking first when that merges two | renames it on these items |
  | `--tag`, `--` | `--tag` deletes a tag everywhere, after asking | `--` takes every tag off these items |
- **Trash:** deleting moves an item to the trash, where it can be restored. Nothing is removed for
  good until the trash is **emptied by hand**; no automatic expiry.
- **Login:** one password, no usernames. A successful login sets a long-lived cookie so each device
  only logs in once.
- **Downloader:** paste a URL and pick a destination folder; the server fetches the file and saves
  it there, with progress shown in the UI. The download runs server-side, so it keeps going when the
  browser tab closes. Plain HTTP(S) only, with the standard library (no yt-dlp-style site
  support); three run at once and the rest wait their turn. The list lives in memory, so a restart
  empties it and cuts running downloads short. In the page it is "Upload from link" under the "+"
  button's arrow (or L), into the folder on screen unless another is picked; a downloads button
  with a count of those running appears beside it while the list has any.
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
pytest, Docker.

```bash
uv sync                                        # install the locked dependencies into .venv
BINNY_PASSWORD=dev uv run app.py --debug       # dev server on 127.0.0.1:5002 (--host / --port to change)
BINNY_DATA_DIR=/path/to/data BINNY_PASSWORD=dev uv run app.py   # data directory (default: ./data)

uv run pytest                                  # full suite (use `uv run`, not bare pytest)
uv run pytest tests/test_auth.py::test_sign_out -x
node --check binny/static/js/<file>.js         # frontend syntax check; there is no JS test suite

docker compose -f docker/compose.yml up --build   # BINNY_PASSWORD goes in docker/.env
```

The app refuses to start without `BINNY_PASSWORD`, including `uv run app.py --help`.

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
| `binny/__init__.py` | `create_app()`: cookie settings, `init_db()`, the cross-site write guard, the login guard, blueprints, and the startup scan of `data/files/` in a background thread. |
| `binny/config.py` | Paths, read once from the environment (`BINNY_DATA_DIR`, `BINNY_PASSWORD`). |
| `binny/db.py` | Schema (`init_db()`, idempotent) and `get_db()`, a short-lived connection per use. |
| `binny/auth.py` | The login: sign-in and sign-out routes, `require_login()` in front of everything else, and the cookie signing key. |
| `binny/views.py` | The page, the stored files (`/files/<path>`: shown or downloaded, a folder as a zip) and `POST /zip` for a multi-selection. |
| `binny/storage.py` | The one place a client path becomes a real one (`clean_path()`, `resolve()`), the rules for names (`check_name()`), `free_name()` for "name (1).ext", and `partial_in()` for the hidden file a new file is written to. |
| `binny/index.py` | The `entries` table, Binny's index of the disk: `refresh()` on every listing, `index_tree()` on start, `record()` for what the app adds, `move_rows()` for renames and moves, folder totals. Its scans delete partial files left behind. |
| `binny/tags.py` | The `tags` table: `clean()` for a tag from the client, `of()` and `attach()` to read them, and `move()` and `drop()`, which keep tags with an item through renames, moves and the trash. |
| `binny/archive.py` | Zips streamed to the browser while they're written. |
| `binny/downloader.py` | Upload from link: the download jobs, kept in memory, each run in a thread of its own. |
| `binny/api/` | One Flask blueprint per resource, all under `/api`: `files` (list, upload, rename, move), `folders` (the sidebar's tree and counts, every folder for the download picker, new folder), `trash` (trash, restore, delete forever, empty), `tags` (tags in use, tagging, renaming or deleting a tag everywhere, and `/api/search`), `downloads` (start, list, cancel, clear the finished). `common.py` turns request arguments into checked paths and names or aborts with the message the UI shows. |
| `binny/templates/` | `base.html` (head, the theme script, the icon sprite), `login.html`, `index.html` (the app shell). |
| `binny/static/js/` | ES modules, one per concern, entry `main.js` loaded with `<script type="module">`: `api.js`, `ui.js` (escaping, formatting, the toast, the question dialog), `paths.js`, `state.js`, `selection.js` (click, Ctrl- and Shift-click selection for any table), `explorer.js` (breadcrumb and file table, or a search's results), `trash.js` (trashing with undo, and the trash view), `sidebar.js` (folder tree), `tags.js` (the tags in use, the tag syntax the search box and the tag editor share, the sidebar's Tags), `search.js` (the search box), `tagger.js` (the tag editor), `upload.js`, `downloads.js` (the "+" button's menu and the downloads panel), `move.js`, `drop.js` (all drag and drop), `shortcuts.js`, `theme.js`. |
| `binny/static/css/` | `cavecomputing.css` (the design system's `bundle.css`, copied unchanged) and `style.css` (the tokens and Binny's own layout). |
| `tests/` | pytest, one file per blueprint or module. |
| `docker/` | `Dockerfile`, `compose.yml` and `entrypoint.sh`, as in imgy: gunicorn with one gthread worker on port 5002, and `/data` handed to `PUID`:`PGID` (`setpriv`, so no apt layer). |

Fill in the "Owns" column with real names as modules land, and add the rules the code can't tell
you on its own under it:

- **Run one worker process** (threads are fine). `storage.NAME_LOCK`, which stops two writers
  claiming the same free name, lives in memory, and so do the downloader's jobs.
- `create_app(index_files=False)` skips the startup scan; only the tests use it.

### Frontend conventions

- Native ES modules only, `import`/`export` with relative paths. No bundler, no framework, no
  globals except what `main.js` deliberately wires up.
- Call the server through `api.js`, which throws on errors and shows them to the user (`quiet` for
  polling, which tries again soon anyway). The toast is a popover so it shows above an open dialog;
  a dialog whose action fails stays open (`ask()`).
- Modules that bind listeners export an `initX()`; `main.js` calls them in order.
- A module that changes files calls `filesChanged()` (`state.js`); the explorer and the sidebar
  reload on that event. Don't reach into another module to redraw it.
- Folders are addressed by the hash (`#/photos/2026`, the trash is `#trash`, a search is
  `#search/<query>`), so back, forward and reload work and any of them can be bookmarked. A search
  keeps `state.folder`, so uploads and new folders still go where it started.
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
  connection never leaves a half-written file under the real name (`storage.partial_in()`). A
  partial file nothing has written to for a day was cut short by a restart, and scanning its folder
  deletes it.
- Stream uploads and downloads; never read a whole file into memory. Downloads should support
  range requests so large files and media resume and seek.
- A taken name gets " (1)", " (2)" … (`free_name()`); an upload or a move never overwrites. Only a
  rename refuses a taken name, because the user typed it.
- A stored file opens in the browser only if that can't run anything as part of Binny: media, PDFs
  and text, with all text (HTML too) sent as `text/plain` and SVG sandboxed. Everything else
  downloads (`views.shown_as()`).
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
  the app show up on the next listing of their folder or of a folder above it whose modified time
  moved (`index.refresh()`), and everywhere after a restart; the app must cope with that rather than
  assume it owns the tree.
- **`data/binny.db` holds only metadata, never file contents**, keyed by the path relative to
  `data/files/`. It exists so listing, sorting and tag search never have to walk the disk:
  - `entries`: a row per visible file and folder with its path, parent folder, size in bytes and
    modified time (`mtime`). A folder's item count and total size are summed from the rows below
    it in one query each (the primary key serves the range), never stored, so they can't drift.
    The kind (image, video, document, archive…) comes from the extension when listing
    (`storage.KINDS`);
  - `tags`: a row per (path, tag), indexed by tag, so a tag search is one indexed query. Tags go
    with an item the app renames, moves or trashes (`tags.move()`), and whatever the app creates
    starts clean (`tags.drop()`). Something moved or deleted outside the app leaves its tags at
    the old path, where nothing shows them (listings and searches only see indexed paths), and
    they come back if it returns.

  Treat it as a cache of the disk plus the things only it knows (tags, trash records). `entries`
  can always be rebuilt from `data/files/`; comparing `mtime` and size against the disk is how
  outside changes are noticed. Tags and trash records cannot be rebuilt, so never drop them in a
  resync.
- Because rows are keyed by path, anything the database knows about a file must survive the file
  disappearing or appearing from outside the app. A move or rename through the app updates its rows in the same request; a row whose file is
  gone is stale, not an error.
- **Trash is `data/files/.trash/`.** The app hides it (and every dot-folder) from listings, tag
  search and folder totals, and never lets an upload, move, rename or download target it except
  through the trash routes. A trashed item keeps its name behind the time it was trashed
  (`<time_ns>_<name>`), and a `trash` row remembers where it came from and its size, so restore
  can put it back (making its folder again if that's gone; a taken name gets " (1)") and the trash
  never walks a folder to size it. Trashed items keep their tags under `.trash/<name>`; renaming or
  deleting a tag everywhere reaches them too. Anything in `.trash/` is trash,
  rows or not: something put there by hand shows in the trash and restores to the top folder.

### The downloader fetches URLs on the server's behalf

That makes it the one place where Binny reaches out to the network, so:

- Only `http` and `https`, in the link and in every redirect (`downloader.Redirects`). It can reach
  anything the server can, LAN addresses included; that's deliberate for one user behind a login.
- Stream the response into a partial file in the destination folder and rename it into place, the
  same as an upload. Never buffer it in memory.
- Take the filename from `Content-Disposition` or the URL it ended at, then make it one
  `check_name()` accepts (`safe_name()`); never let the remote server choose the path.
- One download can't fill the disk: one whose size won't fit is refused, and any stops before it
  would leave less than `KEEP_FREE` (1 GB). Connecting and each read time out after 30 seconds, so
  a dead server can't hold a slot.
- Run it off the request thread (it can take hours) and keep its state where the page can poll it
  (`GET /api/downloads`). The threads are daemons, so a restart never waits for a download.

### Access

Binny sits on the LAN or Tailscale behind a Caddy reverse proxy, never on the public internet, but
it still has a login because anything on the tailnet can reach it.

- **Every route except the login page and its static assets requires the session cookie**,
  including file downloads and thumbnails. A new route is behind the check by default, not opted in.
- The password is the `BINNY_PASSWORD` environment variable, so a Docker container sets it in
  `compose.yml` or an `.env` file. Never store it in the repo, the database or the data directory.
  Refuse to start when it is unset or empty rather than running without a login. Compare it in
  constant time.
- The cookie (`binny_session`; cookies ignore the port, so the name must not collide with sibling
  apps) is `HttpOnly`, `SameSite=Lax`, and lasts 400 days when "Keep this device signed in" is
  ticked. It is signed with a key derived from a random `secret_key` row in `settings` *and* the
  password, so **changing `BINNY_PASSWORD` signs every device out**; so does deleting that row and
  restarting.
- A wrong password waits a second before answering, which makes guessing slow.
- Behind Caddy, `X-Forwarded-Proto` (via `ProxyFix`) decides whether the cookie is `Secure`: over
  HTTPS it is, over plain HTTP on the LAN it can't be or it would never come back. Caddy passes the
  Host header through unchanged by default.
- Writes from another site's page are refused (`reject_cross_site_writes()`, the same check as
  imgy and cozy), so keep state-changing routes on POST/PUT/DELETE.

## Testing gotchas

[tests/conftest.py](tests/conftest.py)'s `data_dir` fixture points `binny.config`'s paths and
password at a temporary directory with `monkeypatch`. That only works because every module reads
them as `config.FILES_DIR` at call time; a `from .config import FILES_DIR` binds the value at
import, the patch never reaches it, and the tests quietly start writing into the real `data/`.
Use the `client` fixture for a signed-in test client and `anon` for one that isn't, `files` for the
temporary `data/files/`, and conftest's `upload()` and `listing()` helpers to drive the API.
