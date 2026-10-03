"""Listing a folder, and uploading, renaming and moving files and folders."""
import os
import shutil
import uuid

from flask import Blueprint, abort, request

from .. import index, storage
from ..db import get_db
from .common import folder_arg, item_arg, items_arg, json_body, name_arg

bp = Blueprint('files', __name__)


@bp.get('/list')
def list_folder():
    """What's in a folder, synced with the disk first so changes made outside the app show up."""
    folder, _ = folder_arg(request.args.get('folder', ''))
    index.refresh(folder)
    with get_db() as conn:
        rows = conn.execute('SELECT * FROM entries WHERE parent = ?', (folder,)).fetchall()
        return {'folder': folder, 'items': index.describe(conn, rows)}


@bp.put('/upload')
def upload():
    """Stream one file into a folder: PUT the raw bytes to ?folder=<folder>&path=<name>.

    path may start with subfolders ("trip/day 1/a.jpg") when a dropped folder is uploaded; missing
    ones are created and existing ones are used as they are. The bytes go to a hidden .part file
    beside the target and are renamed into place once complete, so a dropped connection never
    leaves a half-written file under the real name. A taken name gets " (1)", never an overwrite.
    """
    folder, _ = folder_arg(request.args.get('folder', ''))
    *subfolders, name = [name_arg(part) for part in request.args.get('path', '').split('/')]
    folder = '/'.join([folder, *subfolders]).strip('/')
    try:
        created = storage.make_folders(folder)
        target_dir = storage.resolve(folder)
    except storage.InvalidPath as e:
        abort(409, str(e))

    partial = target_dir / f'.binny-{uuid.uuid4().hex}.part'
    try:
        with open(partial, 'wb') as out:
            shutil.copyfileobj(request.stream, out, 1 << 20)
        with storage.NAME_LOCK:
            name = storage.free_name(target_dir, name)
            os.rename(partial, target_dir / name)
    finally:
        partial.unlink(missing_ok=True)

    path = storage.child(folder, name)
    with get_db() as conn:
        for added in created + [path]:
            index.record(conn, added)
        conn.commit()
    return {'path': path}, 201


@bp.post('/rename')
def rename():
    data = json_body()
    path, item = item_arg(data.get('path'))
    name = name_arg(data.get('name'))
    new_path = storage.child(storage.parent_of(path), name)
    if new_path != path:
        with storage.NAME_LOCK:
            if os.path.lexists(item.parent / name):
                abort(409, f'There is already a "{name}" here')
            os.rename(item, item.parent / name)
        with get_db() as conn:
            index.move_rows(conn, path, new_path)
            conn.commit()
    return {'path': new_path}


@bp.post('/move')
def move():
    """Move files and folders into another folder. A taken name gets " (1)", never an overwrite."""
    data = json_body()
    items = items_arg(data.get('paths'))
    dest, dest_dir = folder_arg(data.get('to'))
    for path, _ in items:
        if dest == path or dest.startswith(path + '/'):
            abort(400, f'"{storage.name_of(path)}" can\'t go inside itself')

    moved = renamed = 0
    for path, item in items:
        if storage.parent_of(path) == dest:
            continue
        with storage.NAME_LOCK:
            name = storage.free_name(dest_dir, item.name, item.is_dir())
            os.rename(item, dest_dir / name)
        with get_db() as conn:
            index.move_rows(conn, path, storage.child(dest, name))
            conn.commit()
        moved += 1
        renamed += name != item.name
    return {'moved': moved, 'renamed': renamed}
