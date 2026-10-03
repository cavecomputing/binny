"""The trash: moving files and folders into data/files/.trash/, restoring them, deleting them for good.

A trashed item keeps its name behind the time it was trashed, "<time_ns>_<name>", and a row in the
trash table remembers where it came from. Its tags wait under ".trash/<that name>". Anything in
.trash/ counts as trash, including things put there by hand; those restore to the top folder.
"""
import os
import shutil
import time

from flask import Blueprint, abort

from .. import config, index, storage, tags
from ..db import get_db
from .common import items_arg, json_body

bp = Blueprint('trash', __name__)


def trash_entries():
    """The visible entries of data/files/.trash/."""
    try:
        with os.scandir(config.TRASH_DIR) as entries:
            return [entry for entry in entries if not entry.name.startswith('.')]
    except FileNotFoundError:
        return []


def split_trash_name(trash_name):
    """(trashed-at seconds, original name) from "<time_ns>_<name>"; (None, trash_name) otherwise."""
    stamp, _, name = trash_name.partition('_')
    return (int(stamp) / 1e9, name) if stamp.isdigit() and name else (None, trash_name)


def names_arg(names):
    """Names of things in the trash the client picked, de-duplicated in order."""
    if not isinstance(names, list) or not names:
        abort(400, 'Pick at least one item in the trash')
    for name in names:
        if not isinstance(name, str) or not name or '/' in name or '\0' in name or name.startswith('.'):
            abort(400, 'Invalid name')
        if not os.path.lexists(config.TRASH_DIR / name):
            abort(404, 'That is no longer in the trash')
    return list(dict.fromkeys(names))


def remove(path):
    """Delete a file, or a folder and everything in it, for good."""
    if path.is_dir() and not path.is_symlink():
        shutil.rmtree(path)
    else:
        path.unlink(missing_ok=True)


@bp.get('/trash')
def list_trash():
    """What's in the trash, last trashed first."""
    with get_db() as conn:
        records = {row['name']: row for row in conn.execute('SELECT * FROM trash')}
    items = []
    for entry in trash_entries():
        trashed_at, name = split_trash_name(entry.name)
        record = records.get(entry.name)
        st = entry.stat(follow_symlinks=False)
        is_dir = entry.is_dir(follow_symlinks=False)
        items.append({
            'name': entry.name,
            'original': record['original'] if record else name,
            'is_dir': is_dir,
            'size': record['size'] if record else (0 if is_dir else st.st_size),
            'trashed_at': trashed_at or st.st_mtime,
            'kind': storage.kind_of(name, is_dir),
        })
    items.sort(key=lambda item: item['trashed_at'], reverse=True)
    return {'items': items}


@bp.post('/trash')
def trash():
    """Move files and folders to the trash. Returns their names there, which undo restores."""
    items = items_arg(json_body().get('paths'))
    config.TRASH_DIR.mkdir(exist_ok=True)
    names = []
    for path, item in items:
        is_dir = item.is_dir()
        if is_dir:
            index.refresh(path)  # so its size counts what's in it now
        with get_db() as conn:
            size = index.folder_totals(conn, path)[1] if is_dir else item.stat().st_size
            name = f'{time.time_ns()}_{item.name}'
            os.rename(item, config.TRASH_DIR / name)
            index.forget(conn, path)
            tags.move(conn, path, f'.trash/{name}')
            conn.execute('INSERT INTO trash (name, original, size) VALUES (?, ?, ?)', (name, path, size))
            conn.commit()
        names.append(name)
    return {'names': names}


@bp.post('/trash/restore')
def restore():
    """Put trashed items back where they were. A folder they were in is made again if it's gone,
    and a taken name gets " (1)"."""
    restored = []
    for name in names_arg(json_body().get('names')):
        source = config.TRASH_DIR / name
        is_dir = source.is_dir() and not source.is_symlink()
        with get_db() as conn:
            record = conn.execute('SELECT original FROM trash WHERE name = ?', (name,)).fetchone()
            original = record['original'] if record else split_trash_name(name)[1]
            parent = storage.parent_of(original)
            try:
                created = storage.make_folders(parent)
                folder = storage.resolve(parent)
            except storage.InvalidPath as e:
                abort(409, f'Can\'t restore "{storage.name_of(original)}": {e}')
            with storage.NAME_LOCK:
                restored_name = storage.free_name(folder, storage.name_of(original), is_dir)
                os.rename(source, folder / restored_name)
            path = storage.child(parent, restored_name)
            for added in created + [path]:
                index.record(conn, added)
            for added in created:
                tags.drop(conn, added)
            tags.move(conn, f'.trash/{name}', path)
            conn.execute('DELETE FROM trash WHERE name = ?', (name,))
            conn.commit()
        if is_dir:
            index.index_tree(path)
        restored.append(path)
    return {'restored': restored}


@bp.post('/trash/delete')
def delete_forever():
    names = names_arg(json_body().get('names'))
    for name in names:
        remove(config.TRASH_DIR / name)
    with get_db() as conn:
        conn.executemany('DELETE FROM trash WHERE name = ?', [(name,) for name in names])
        for name in names:
            tags.drop(conn, f'.trash/{name}')
        conn.commit()
    return {'deleted': len(names)}


@bp.post('/trash/empty')
def empty():
    entries = trash_entries()
    for entry in entries:
        remove(config.TRASH_DIR / entry.name)
    with get_db() as conn:
        conn.execute('DELETE FROM trash')
        tags.drop(conn, '.trash')
        conn.commit()
    return {'deleted': len(entries)}
