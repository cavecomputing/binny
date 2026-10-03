"""Request helpers shared by the routes.

They validate what the client sent and abort() with the message the frontend shows, so route
handlers can stay on the happy path.
"""
import os

from flask import abort, request

from .. import storage


def json_body():
    """The request's JSON object, or {} when there is no body. Anything else is a 400."""
    if not request.get_data(cache=True):
        return {}
    data = request.get_json(silent=True)
    if not isinstance(data, dict):
        abort(400, 'Expected a JSON object')
    return data


def cleaned(path):
    """(relative path, absolute path) for a client path, or a 400."""
    try:
        rel = storage.clean_path(path)
        return rel, storage.resolve(rel)
    except storage.InvalidPath as e:
        abort(400, str(e))


def folder_arg(path):
    """An existing folder the client named; '' is the top folder."""
    rel, folder = cleaned(path)
    if folder.is_symlink() or not folder.is_dir():
        abort(404, 'That folder no longer exists')
    return rel, folder


def item_arg(path):
    """An existing file or folder the client named, other than the top folder."""
    rel, item = cleaned(path)
    if not rel:
        abort(400, 'Pick a file or folder')
    if item.is_symlink() or not os.path.lexists(item):
        abort(404, f'"{storage.name_of(rel)}" no longer exists')
    return rel, item


def items_arg(paths, limit=10_000, keep_nested=False):
    """A list of existing files and folders the client named, de-duplicated in order.

    Unless keep_nested is set, an item inside another listed folder is left out: moving, trashing
    or zipping the folder takes it along. (A search can list both.)
    """
    if not isinstance(paths, list) or not paths:
        abort(400, 'Pick at least one file or folder')
    if len(paths) > limit:
        abort(400, f'Pick at most {limit} items at a time')
    items = dict(item_arg(path) for path in paths)
    if not keep_nested:
        items = {rel: item for rel, item in items.items() if not inside_any(rel, items)}
    return list(items.items())


def inside_any(rel, folders):
    """Whether rel is below one of folders."""
    parent = storage.parent_of(rel)
    while parent:
        if parent in folders:
            return True
        parent = storage.parent_of(parent)
    return False


def name_arg(name):
    try:
        return storage.check_name(name)
    except storage.InvalidPath as e:
        abort(400, str(e))
