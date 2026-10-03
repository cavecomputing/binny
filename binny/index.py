"""The file index: a row per visible file and folder under data/files/, so listings, folder totals
and searches never walk the disk.

The disk stays the source of truth. sync_folder() brings one folder's rows in line with it.
refresh() runs on every listing: it syncs the folder, then each folder below it whose modified time
moved, so files added or removed from outside the app show up as far down as the change went. A
file edited in place doesn't move its folder's time, so index_tree() syncs everything on start.
Changes made through the app update their rows in the same request; moves and renames re-key them
with move_rows() instead of rescanning.
"""
import logging
import os
import stat

from . import storage
from .db import get_db

logger = logging.getLogger(__name__)

# Matches a path and everything below it. Bind subtree_args(path). '0' sorts right after '/', so
# the range is exactly the rows under 'path/', and the primary key index serves it.
SUBTREE = '(path = ? OR (path > ? AND path < ?))'


def subtree_args(path):
    return path, path + '/', path + '0'


def entry_row(st):
    """(is_dir, size, mtime) for a stat result, or None for anything but a plain file or folder."""
    if stat.S_ISDIR(st.st_mode):
        return 1, 0, st.st_mtime
    if stat.S_ISREG(st.st_mode):
        return 0, st.st_size, st.st_mtime
    return None


def scan(folder):
    """{path: (is_dir, size, mtime)} for the visible entries directly inside folder, or None if it is gone.

    Hidden entries (.trash, partial uploads) and symlinks are left out, and so are names that aren't
    valid UTF-8, which neither SQLite nor JSON can carry.
    """
    found = {}
    try:
        with os.scandir(storage.resolve(folder)) as entries:
            for entry in entries:
                if entry.name.startswith('.'):
                    continue
                try:
                    entry.name.encode()
                    row = entry_row(entry.stat(follow_symlinks=False))
                except (UnicodeEncodeError, OSError):
                    continue
                if row:
                    found[storage.child(folder, entry.name)] = row
    except (FileNotFoundError, NotADirectoryError):
        return None
    return found


def sync_folder(conn, folder):
    """Bring the rows directly inside folder in line with the disk.

    Returns the subfolders that are new to the index or whose modified time moved, the ones whose
    own contents may have changed since they were last synced.
    """
    on_disk = scan(folder)
    if on_disk is None:
        if folder:
            forget(conn, folder)
        return []
    indexed = {row['path']: (row['is_dir'], row['size'], row['mtime'])
               for row in conn.execute('SELECT path, is_dir, size, mtime FROM entries WHERE parent = ?', (folder,))}
    for path in indexed.keys() - on_disk.keys():
        forget(conn, path)
    changed_folders = []
    for path, row in on_disk.items():
        old = indexed.get(path)
        if old == row:
            continue
        if old and old[0] != row[0]:  # a file became a folder, or the other way round
            forget(conn, path)
        conn.execute('INSERT OR REPLACE INTO entries (path, parent, is_dir, size, mtime) VALUES (?, ?, ?, ?, ?)',
                     (path, folder, *row))
        if row[0]:
            changed_folders.append(path)
    return changed_folders


def refresh(folder):
    """Sync folder, then every folder below it that changed, one short transaction per folder."""
    pending = [folder]
    while pending:
        current = pending.pop()
        with get_db() as conn:
            pending += sync_folder(conn, current)
            conn.commit()


def index_tree(folder=''):
    """Sync folder and every folder below it, changed or not, one short transaction per folder."""
    pending = [folder]
    while pending:
        current = pending.pop()
        with get_db() as conn:
            sync_folder(conn, current)
            conn.commit()
            pending += [row['path'] for row in conn.execute(
                'SELECT path FROM entries WHERE parent = ? AND is_dir', (current,))]


def index_everything():
    """The startup scan, run in a background thread so a big library doesn't hold up the first page."""
    try:
        index_tree()
        logger.info('Indexed data/files/')
    except Exception:
        logger.exception('Indexing data/files/ failed')


def record(conn, path):
    """Index one file or folder just added through the app, without rescanning the folder it is in."""
    row = entry_row(storage.resolve(path).stat(follow_symlinks=False))
    conn.execute('INSERT OR REPLACE INTO entries (path, parent, is_dir, size, mtime) VALUES (?, ?, ?, ?, ?)',
                 (path, storage.parent_of(path), *row))


def forget(conn, path):
    """Drop the rows of path and everything below it."""
    conn.execute(f'DELETE FROM entries WHERE {SUBTREE}', subtree_args(path))


def move_rows(conn, old, new):
    """Re-key the rows of old and everything below it to new, after the disk move. Rows already at new are stale."""
    forget(conn, new)
    start = len(old) + 1
    conn.execute(f'''UPDATE entries
                     SET parent = CASE WHEN path = ? THEN ? ELSE ? || substr(parent, ?) END,
                         path = ? || substr(path, ?)
                     WHERE {SUBTREE}''',
                 (old, storage.parent_of(new), new, start, new, start, *subtree_args(old)))


def folder_totals(conn, path):
    """(items directly inside, bytes in everything below) for an indexed folder; '' is the top folder."""
    count = conn.execute('SELECT COUNT(*) FROM entries WHERE parent = ?', (path,)).fetchone()[0]
    below, args = ('WHERE path > ? AND path < ?', (path + '/', path + '0')) if path else ('', ())
    size = conn.execute(f'SELECT COALESCE(SUM(size), 0) FROM entries {below}', args).fetchone()[0]
    return count, size


def describe(conn, rows):
    """Entry rows as the JSON the explorer draws."""
    items = []
    for row in rows:
        name = storage.name_of(row['path'])
        item = {'path': row['path'], 'name': name, 'is_dir': bool(row['is_dir']), 'size': row['size'],
                'mtime': row['mtime'], 'kind': storage.kind_of(name, row['is_dir'])}
        if row['is_dir']:
            item['items'], item['size'] = folder_totals(conn, row['path'])
        items.append(item)
    return items
