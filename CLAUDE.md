# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Binny is a small, single-user file hosting app for personal use: a file explorer in the browser with
folders, uploads, downloads, moves, renames, tags and tag search, and a trash. It runs on the LAN or
over Tailscale behind Caddy and is never exposed to the internet. Its tag handling follows
[imgy](https://github.com/cavecomputing/imgy).

The app is early — most of it does not exist yet — so sections marked _(open)_ record decisions
still being made. Update them as they settle rather than working around them.

## What it does, and what it deliberately doesn't

- **Navigation has three parts, each with one job:** the sidebar folder tree for moving sideways
  (it opens only the branch you are in; other branches open by their arrow), the breadcrumb for
  the exact path and moving up, and the file list for moving down. The sidebar is always shown
  beside the list, never collapsed, but its width is dragged at the handle on its edge (or set with the
  arrow keys, double-click to reset) and remembered per device; on phones it is a drawer (opened by the top bar's button, covering the whole window, closed by
  its X), which holds the brand, the theme switch and sign-out as the sidebar does on wider screens. Also on phones, the search
  box is a dock along the bottom (its suggestions open upward) so the top bar can keep the wordmark.
- **Explorer:** nested folders; create, rename, move (drag-and-drop and a "move to" picker),
  upload (button and drag-and-drop, many files at once), download (single files; folders and
  multi-selections as a zip). The rows move by keyboard, with the arrow keys or vim's: Up/Down or
  `k`/`j` select the previous or next row (Shift extends), `gg`/`G` or Home/End jump to the ends,
  Right or Enter opens the selected item and Left or `h` goes up a folder, in the trash too except
  opening and going up. `l` stays "Upload from link", so it isn't vim's right.
  On the highlighted rows: `t` tags, `r` (or F2) renames, `m` moves, `s` (save) downloads (a selection as a
  zip) and Del trashes.
- **Tags:** any file or folder can carry tags (the list doesn't show them: they are reached through
  the "Tags" button in a row's menu, or `t`), and the search box finds things anywhere by tag or
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
single-binary, single-process app with no frontend build, and it stays approachable only while
changes stay small.

- Prefer editing existing code over adding new code, and a few lines at the right call site over a
  new helper, module or class. Add a file only when something forces it.
- Reuse what is already here. A second implementation of something the repo already does is the
  most expensive kind of code.
- **No new dependency without asking first.**
- Leave out what nothing needs yet: config knobs, feature flags, an interface or abstraction with one
  caller, error handling for code that can't fail, re-validation of data the route already validated.
- Deleting code is a legitimate way to finish a task. Say so when the fix turns out to be a removal.

This governs the amount of code, not the amount of work. Deliver everything that was asked and run
the verification the change calls for.

## Naming

A name is the cheapest documentation in the file. Make it a concise nameplate for what the thing is
or does — long enough to be unambiguous where it is *used*, short enough to read at a glance.

- Prefer the specific noun to the category: `stored_path`, `size_bytes`, `file_ids` over `value`,
  `data`, `items`.
- Name a function for what it returns or does, not how it works — `FreeName()`, `Usage()`,
  `formatSize()`.
- Don't encode the type or the scope in the name (`fileObjDict`, `tmpList`), and don't abbreviate
  past recognition.
- Follow the language you are editing — Go's `MixedCaps` (exported only when another package uses
  it, and no stutter: `storage.CleanPath`, not `storage.StorageCleanPath`), `camelCase` in JS — over
  any preference of your own.

Renaming existing code is its own task. Don't fold a rename into an unrelated change, where it
buries the real diff under noise.

## Run / test

Go backend, vanilla-JS frontend with no build step: SQLite, one binary, Docker.

```bash
BINNY_PASSWORD=dev go run .                    # dev server on 127.0.0.1:5000 (-host / -port to change)
BINNY_DATA_DIR=/path/to/data BINNY_PASSWORD=dev go run .   # data directory (default: ./data)
go build                                       # ./binny, with the page and static files inside it

go test ./...                                  # full suite, the end-to-end test included
go test -run TestEndToEnd .                    # just the end-to-end test: a day in the life over real HTTP
go test ./internal/server -run TestRenameRefusesATakenName
go vet ./... && gofmt -l .                     # both must print nothing
node --check web/static/js/<file>.js           # frontend syntax check; there is no JS test suite

docker compose -f docker/compose.yml up --build   # BINNY_PASSWORD goes in docker/.env
```

The app refuses to start without `BINNY_PASSWORD`. The static files and templates are embedded at
build time, so a change to them shows after a restart of `go run .`.

The Go version is pinned by the `go` line in `go.mod` (the `go` command fetches it when it's
newer than the one installed) and by the `golang` image in `docker/Dockerfile`; change them
together. Dependencies are in `go.mod` and `go.sum`: change them with `go get` and `go mod tidy`
and commit both files together. The only one is `modernc.org/sqlite`, a SQLite that needs no cgo,
so the binary builds anywhere with `CGO_ENABLED=0`.

### How much verification a change needs

Run the tests that cover what was touched, and the full suite before anything the user is likely to
commit. When in doubt, run all of it.

**Booting the dev server is not required after every change.** Start it when the change can only be
confirmed in a browser — layout, upload progress, drag-and-drop, mobile behaviour, large-file
downloads — or when asked. For backend logic, docs and comments, skip it and say so. Never claim a
change was verified in the app when it wasn't.

## Architecture

One Go process: `net/http` with the standard `ServeMux` (method and wildcard patterns), the
page from `html/template`, and the frontend as vanilla JS with no build step. The layout follows
Go's own guidance for a server: `main.go` at the root, everything else in packages under
`internal/`, one thing per package, named for what it owns, so finding the code for a feature
never takes a search. Keep it that way — a new feature gets its own file (or package, if other
packages need it) rather than growing a neighbour. Packages depend downwards only:
`server` → `auth`, `downloader`, `archive` → `db` → `storage`.

| Path | Owns |
|---|---|
| `main.go` | Entry point: reads `BINNY_DATA_DIR`, `BINNY_PASSWORD` and the `-host`/`-port` flags, opens the disk and the database, starts the startup scan of `data/files/` in a goroutine, and serves. No logic lives here. |
| `internal/storage/` | The one way to the files: `Disk`, an `os.Root` on `data/files/`, so no path leads out of it (`Plain()` for what counts as an item — a file or folder, never a symlink, pipe or device — `IsFolder()`, `FreeName()` for "name (1).ext" under `NameLock`, `MakeFolders()`, `Usage()`); the rules for client paths and names (`CleanPath()`, `CheckName()`, `Numbered()`, `PartialName()` for the hidden file a new file is written to); kinds and content types by extension (`KindOf()`, `ContentType()`); and `Problem()`, what the system said went wrong. |
| `internal/db/` | `binny.db`: the schema (`Open()`, idempotent) and one `*sql.DB`; `Update()` runs a function in a transaction, and `DB` and `Tx` share the query methods. `entries.go` is Binny's index of the disk (`Refresh()` on every listing, `IndexTree()` on start, `Record()` for what the app adds, `MoveRows()` for renames and moves, `FolderTotals()`, `List()`); its scans delete partial files left behind. `tags.go` is the tags (`CleanTag()`, `Tags()`, `MoveTags()` and `DropTags()`, which keep tags with an item through renames, moves and the trash, renaming or deleting a tag everywhere, and `Search()`), `trash.go` the trash records, `folders.go` the sidebar's tree. |
| `internal/auth/` | The login: the sign-in and sign-out handlers, `Require()` in front of everything else, and the signed session cookie. |
| `internal/archive/` | Zips streamed to the browser while they're written. |
| `internal/downloader/` | Upload from link: `Manager` keeps the download jobs in memory and runs each in a goroutine of its own; `names.go` checks the link (`ParseURL()`) and names the file (`safeName()`). |
| `internal/server/` | Every route, listed in `server.go`'s `New()`, with `handle()`, which turns a handler's error into the status and message the UI shows. One file per resource: `files.go` (list, upload, rename, move), `folders.go` (the sidebar's tree and counts, every folder for the download picker, new folder), `trash.go` (trash, restore, delete forever, empty), `tags.go` (tags in use, tagging, renaming or deleting a tag everywhere, and `/api/search`), `downloads.go` (start, list, cancel, clear the finished), `views.go` (the stored files, `/files/<path>`: shown or downloaded, a folder as a zip, and `POST /zip` for a multi-selection). `args.go` turns request arguments into checked paths and names or fails with the message the UI shows. |
| `web/` | `web.go` embeds the rest into the binary: `Page()` parses a template, `Static()` serves the static files with ETags. |
| `web/templates/` | `base.html` (head, the theme script, the icon sprite), `login.html`, `index.html` (the app shell). |
| `web/static/js/` | ES modules, one per concern, entry `main.js` loaded with `<script type="module">`: `api.js`, `ui.js` (escaping, formatting, the toast, the question dialog), `paths.js`, `state.js`, `selection.js` (click, Ctrl- and Shift-click and keyboard selection for any table), `explorer.js` (breadcrumb and file table, or a search's results), `trash.js` (trashing with undo, and the trash view), `rowmenu.js` (the "…" menu at the end of each row, in the file list and the trash: a row's actions behind one button so a long name keeps its room), `sidebar.js` (folder tree, and the handle that sets its width), `tags.js` (the tags in use, the tag syntax the search box and the tag editor share, the sidebar's Tags), `search.js` (the search box), `tagger.js` (the tag editor), `upload.js`, `downloads.js` (the "+" button's menu and the downloads panel), `move.js`, `drop.js` (all drag and drop), `shortcuts.js`, `theme.js`. |
| `web/static/` | Also `manifest.webmanifest` and the `icon-*.png` and `apple-touch-icon.png` it and `base.html` name, which make Binny installable as an app (standalone display; the page fills the window edge to edge everywhere, so it needs nothing special there). They are the favicon's folder rendered at those sizes (the maskable one smaller, for its safe zone); redo them with it. |
| `web/static/css/` | `style.css`: the tokens, the shared components and every view's layout. |
| `*_test.go` | Beside the code they test, one per file or concern; `internal/server/server_test.go` has the helpers. `e2e_test.go` at the root drives the whole app over real HTTP through `main.go`'s `open()`. |
| `docker/` | `Dockerfile` (the binary built in `golang`, run in `debian:trixie-slim`), `compose.yml` and `entrypoint.sh`, as in imgy: port 5000, and `/data` handed to `PUID`:`PGID` (`setpriv`, so no apt layer). |
| `assets/` | The README's screenshots. |

Fill in the "Owns" column with real names as code lands, and add the rules the code can't tell
you on its own under it:

- **Run one process.** `storage.Disk.NameLock`, which stops two writers claiming the same free
  name, lives in memory, and so do the downloader's jobs.
- No package-level state: what a handler needs comes in through `server.New()`, so tests build a
  whole app on a temporary directory.
- Handlers return an error rather than writing one: `fail(status, message)` for what the user
  should read, anything else is logged and becomes a 500 (a disk error says what the disk said).

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
- The look is Binny's own, quiet and plain: hairline borders, roomy rows, one teal accent. It no
  longer uses the cavecomputing design system. Everything lives in `style.css`: the tokens on
  `:root` (dark) and `[data-theme="light"]`, then a few shared components (`btn`, `icon-btn`,
  `input`, `card`, `card-header`, `tag`, `chip`, `badge`, `callout`, `eyebrow`, `navrow`) that
  every view builds on before writing new ones.
- Colors come from the tokens; never hardcode them. Two have a meaning: `--accent` (teal) for
  focus, selection, active rows, folders and the main action, and `--warm` for anything
  destructive. There is no red. Everything else is `--text`, `--muted` or `--faint` on `--bg`,
  `--panel`, `--raised` or `--overlay` (menus, the toast, the selection bar). No web fonts: Inter
  where it's installed, else the system sans, and the system mono for keys and code.
- On wide screens the sidebar holds the brand and, at its foot, Trash, the theme switch and
  sign-out; the main column has the search and the "+" split button, then the path above the
  folder's name as the title, and the selection bar floats over the bottom of the list. The checkboxes show on hover or once something is selected (always on touch).
- **Dark is the default theme**, whatever the OS prefers. The choice lives in `localStorage`
  (`binny-theme`), so each device keeps its own; never sync it through the server. Apply it in a
  tiny inline script in `<head>` before the stylesheet paints, so a light-theme device doesn't
  flash dark on load. Wrap storage access in `try` so a blocked `localStorage` still gets dark.
- **Logo and favicon match the sibling apps** (cozy, imgy, campfire) so they sit together in a tab
  bar: a `#282828` rounded square with a 24px stroked line icon in one accent, 2.2 stroke, round
  caps. Cozy is yellow, imgy green and campfire orange; Binny's is a folder in aqua, `#6bc7bb`.
  That is Binny's own color, not the design system's `accent-aqua` (`#8ec07c`), which reads as a
  second green beside imgy. It is sized like them too: a folder is wider than tall, so at their
  width it looked small, and the favicon draws it 1.15× larger (stroke kept at 2.2) to cover as
  much of the tile as their glyphs do. Size a new glyph by the area it covers, not its width.
  [web/static/favicon.svg](web/static/favicon.svg) is the source and
  `favicon.png` (256px) is rendered from it; change both together. In the page, the brand is
  the sprite's folder in `--accent-ink` on an `--accent` tile, beside the `wordmark`.
- The [interface mockup](https://claude.ai/artifact/XUjeUxP9dUYAQDFV9N6kXj) is the reference for
  the look and layout: files, search, the tag editor, trash, sign-in, and the phone with its
  drawer. The "+" split button uploads files on a click; its arrow opens "Upload from link".

### Files on disk are the user's data

The stored files are the whole point of the app, so anything that writes, moves or deletes them is
held to a higher bar than the rest of the code:

- **Never trust a client-supplied filename or path.** Every path goes through `storage.CleanPath()`
  (no `..`, no hidden parts) and reaches the disk only through `storage.Disk`, whose `os.Root`
  refuses anything that escapes `data/files/`, symlinks out included (and every absolute symlink,
  even one pointing inside: links in there must be relative to be followed). This needs a test.
- Write uploads to a temp file in the same filesystem and rename into place, so a dropped
  connection never leaves a half-written file under the real name (`storage.PartialName()`). A
  partial file nothing has written to for a day was cut short by a restart, and scanning its folder
  deletes it.
- Stream uploads and downloads; never read a whole file into memory. Downloads should support
  range requests so large files and media resume and seek.
- A taken name gets " (1)", " (2)" … (`FreeName()`); an upload or a move never overwrites (and
  `os.Root.Rename` would, so always take a free name under `NameLock`). Only a rename refuses a
  taken name, because the user typed it.
- A stored file opens in the browser only if that can't run anything as part of Binny: media, PDFs
  and text, with all text (HTML too) sent as `text/plain` and SVG sandboxed. Everything else
  downloads (`server.shownAs()`).
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
  moved (`db.Refresh()`), and everywhere after a restart; the app must cope with that rather than
  assume it owns the tree.
- **`data/binny.db` holds only metadata, never file contents**, keyed by the path relative to
  `data/files/`. It exists so listing, sorting and tag search never have to walk the disk:
  - `entries`: a row per visible file and folder with its path, parent folder, size in bytes and
    modified time (`mtime`). A folder's item count and total size are summed from the rows below
    it in one query each (the primary key serves the range), never stored, so they can't drift.
    The kind (image, video, document, archive…) comes from the extension when listing
    (`storage.KindOf()`);
  - `tags`: a row per (path, tag), indexed by tag, so a tag search is one indexed query. Tags go
    with an item the app renames, moves or trashes (`MoveTags()`), and whatever the app creates
    starts clean (`DropTags()`). Something moved or deleted outside the app leaves its tags at
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

- Only `http` and `https`, in the link and in every redirect (the client's `CheckRedirect`). It can reach
  anything the server can, LAN addresses included; that's deliberate for one user behind a login.
- Stream the response into a partial file in the destination folder and rename it into place, the
  same as an upload. Never buffer it in memory.
- Take the filename from `Content-Disposition` or the URL it ended at, then make it one
  `CheckName()` accepts (`safeName()`); never let the remote server choose the path.
- One download can't fill the disk: one whose size won't fit is refused, and any stops before it
  would leave less than `keepFree` (1 GB). Connecting and each read time out after 30 seconds, so
  a dead server can't hold a slot.
- Run it off the request (it can take hours) and keep its state where the page can poll it
  (`GET /api/downloads`). Nothing waits for a download on exit, so a restart never waits for one.

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
  ticked, renewed once a day while the device is in use. It holds when it was issued, signed with
  HMAC-SHA256 under a key derived from a random `secret_key` row in `settings` *and* the password,
  so **changing `BINNY_PASSWORD` signs every device out**; so does deleting that row and
  restarting.
- A wrong password holds off every sign-in, from any device, for a second (`Auth.nextTry`), so
  guessing goes at one try a second however many requests run at once. Already signed-in devices
  aren't affected.
- Behind Caddy, `X-Forwarded-Proto` decides whether the cookie is `Secure`: over
  HTTPS it is, over plain HTTP on the LAN it can't be or it would never come back. Caddy passes the
  Host header through unchanged by default.
- Writes from another site's page are refused (`http.CrossOriginProtection`, the same check as
  imgy and cozy: `Sec-Fetch-Site`, else `Origin` against the host), so keep state-changing routes
  on POST/PUT/DELETE.

## Testing gotchas

Tests use the standard `testing` package only, with table tests for cases. The server tests in
[internal/server/server_test.go](internal/server/server_test.go) build the whole app on
`t.TempDir()`: `newApp(t)` for a signed-in one and `openApp(t, dir, password)` for one that isn't
(or for a restart on the same directory); `a.do()`, `a.upload()` and `a.listing()` drive the API,
and `a.write()`, `a.read()` and `names()` look at the disk. Requests go straight to the handler
through `httptest`, with no network. The download tests serve the far end with `httptest`, and
the login's timing test runs in a `testing/synctest` bubble so its one-second wait takes no time.
The sandbox runs tests as root, so a test can't rely on file permissions being refused.
