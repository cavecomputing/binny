"""The sidebar's folder tree, and new folders."""
import shutil

from flask import Blueprint, abort, request

from .. import config, index, storage
from ..db import get_db
from .common import folder_arg, json_body, name_arg

bp = Blueprint('folders', __name__)

SUBFOLDERS = '''
    SELECT path, EXISTS(SELECT 1 FROM entries AS sub WHERE sub.parent = entries.path AND sub.is_dir) AS has_children
    FROM entries WHERE parent = ? AND is_dir'''


@bp.get('/folders')
def folder_tree():
    """The subfolders of the top folder and of each ?open=<folder>, from the index, plus disk usage."""
    opened = {''}
    for folder in request.args.getlist('open'):
        try:
            opened.add(storage.clean_path(folder))
        except storage.InvalidPath:
            pass
    tree = {}
    with get_db() as conn:
        for folder in opened:
            tree[folder] = [{'path': row['path'], 'name': storage.name_of(row['path']),
                             'has_children': bool(row['has_children'])}
                            for row in conn.execute(SUBFOLDERS, (folder,))]
        files_size = index.folder_totals(conn, '')[1]
    disk = shutil.disk_usage(config.FILES_DIR)
    return {'tree': tree, 'files_size': files_size, 'disk': {'total': disk.total, 'used': disk.used}}


@bp.post('/folders')
def create_folder():
    data = json_body()
    parent, parent_dir = folder_arg(data.get('parent', ''))
    name = name_arg(data.get('name'))
    try:
        (parent_dir / name).mkdir()
    except FileExistsError:
        abort(409, f'There is already a "{name}" here')
    path = storage.child(parent, name)
    with get_db() as conn:
        index.record(conn, path)
        conn.commit()
    return {'path': path}, 201
