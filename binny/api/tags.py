"""Tags in use, tagging files and folders, renaming or deleting a tag everywhere, and search."""
import json
import re

from flask import Blueprint, abort, request

from .. import index, storage, tags
from ..db import get_db
from .common import items_arg, json_body

bp = Blueprint('tags', __name__)

SEARCH_LIMIT = 2000
MAX_SEARCH_TAGS = 50  # each is a join, and SQLite joins at most 64 tables


def tag_arg(tag):
    try:
        return tags.clean(tag)
    except tags.InvalidTag as e:
        abort(400, str(e))


def tags_arg(values):
    """A list of tags from the client, cleaned and de-duplicated in order. Missing means none."""
    if values is None:
        return []
    if not isinstance(values, list):
        abort(400, 'Expected a list of tags')
    return list(dict.fromkeys(tag_arg(tag) for tag in values))


def renames_arg(pairs):
    """[[old, new], ...] from the client, cleaned."""
    if pairs is None:
        return []
    if not isinstance(pairs, list) or not all(isinstance(pair, list) and len(pair) == 2 for pair in pairs):
        abort(400, 'Expected renames as [old, new] pairs')
    return [(tag_arg(old), tag_arg(new)) for old, new in pairs]


def live_count(conn, tag):
    """How many indexed items have tag."""
    return conn.execute('SELECT COUNT(*) FROM tags JOIN entries USING (path) WHERE tag = ?', (tag,)).fetchone()[0]


def words(text):
    """Text as the name search compares it: lowercase, with runs of spaces, "_" and "-" as one space."""
    return re.sub(r'[\s_-]+', ' ', text.lower())


@bp.get('/tags')
def list_tags():
    """Every tag on something in data/files/, with how many items have it, most used first."""
    with get_db() as conn:
        rows = conn.execute('''SELECT tag, COUNT(*) AS count FROM tags JOIN entries USING (path)
                               GROUP BY tag ORDER BY count DESC, tag''').fetchall()
    return {'tags': [{'tag': row['tag'], 'count': row['count']} for row in rows]}


@bp.post('/tags')
def tag_items():
    """Change the tags of files and folders: {paths, add, remove, rename: [[old, new], ...]}.

    A rename only touches the items that have the old tag. Returns every item's tags afterwards.
    """
    data = json_body()
    paths = [path for path, _ in items_arg(data.get('paths'), keep_nested=True)]
    add, remove, renames = tags_arg(data.get('add')), tags_arg(data.get('remove')), renames_arg(data.get('rename'))
    on_items = 'path IN (SELECT value FROM json_each(:paths))'
    args = {'paths': json.dumps(paths), 'add': json.dumps(add), 'remove': json.dumps(remove)}
    with get_db() as conn:
        for old, new in renames:
            conn.execute(f'INSERT OR IGNORE INTO tags (path, tag) SELECT path, :new FROM tags WHERE tag = :old AND {on_items}',
                         {**args, 'old': old, 'new': new})
            if new != old:
                conn.execute(f'DELETE FROM tags WHERE tag = :old AND {on_items}', {**args, 'old': old})
        conn.execute(f'DELETE FROM tags WHERE tag IN (SELECT value FROM json_each(:remove)) AND {on_items}', args)
        conn.execute('''INSERT OR IGNORE INTO tags (path, tag)
                        SELECT item.value, tag.value FROM json_each(:paths) AS item, json_each(:add) AS tag''', args)
        conn.commit()
        return {'tags': tags.of(conn, paths)}


@bp.post('/tags/rename')
def rename_tag():
    """Rename a tag on every item, trashed ones included. An item that has both keeps one."""
    data = json_body()
    old, new = tag_arg(data.get('old')), tag_arg(data.get('new'))
    with get_db() as conn:
        if not conn.execute('SELECT 1 FROM tags WHERE tag = ?', (old,)).fetchone():
            abort(404, f'No tag named "{old}"')
        count = live_count(conn, old)
        if new != old:
            conn.execute('INSERT OR IGNORE INTO tags (path, tag) SELECT path, ? FROM tags WHERE tag = ?', (new, old))
            conn.execute('DELETE FROM tags WHERE tag = ?', (old,))
            conn.commit()
    return {'tag': new, 'items': count}


@bp.post('/tags/delete')
def delete_tag():
    """Take a tag off every item, trashed ones included."""
    tag = tag_arg(json_body().get('tag'))
    with get_db() as conn:
        if not conn.execute('SELECT 1 FROM tags WHERE tag = ?', (tag,)).fetchone():
            abort(404, f'No tag named "{tag}"')
        count = live_count(conn, tag)
        conn.execute('DELETE FROM tags WHERE tag = ?', (tag,))
        conn.commit()
    return {'items': count}


@bp.get('/search')
def search():
    """Files and folders anywhere that have every ?tag=, none of the ?not= tags, and ?name= in their
    name, matched like words(). Folders first, then by path, at most SEARCH_LIMIT of them."""
    include = list(dict.fromkeys(tag.strip().lower() for tag in request.args.getlist('tag') if tag.strip()))
    exclude = list(dict.fromkeys(tag.strip().lower() for tag in request.args.getlist('not') if tag.strip()))
    name = words(request.args.get('name', '')).strip()
    if not (include or exclude or name):
        return {'items': [], 'truncated': False}
    if len(include) > MAX_SEARCH_TAGS:
        abort(400, f'Search for at most {MAX_SEARCH_TAGS} tags at a time')

    # Joins on the included tags let SQLite start from the tag index; the rest filter what's left.
    joins = [f'JOIN tags AS t{i} ON t{i}.path = entries.path AND t{i}.tag = ?' for i in range(len(include))]
    where = ['NOT EXISTS (SELECT 1 FROM tags WHERE tags.path = entries.path AND tags.tag = ?)'] * len(exclude)
    if name:
        where.append('instr(name_words(entries.path), ?)')
    sql = f'''SELECT entries.* FROM entries {' '.join(joins)} {'WHERE ' + ' AND '.join(where) if where else ''}
              ORDER BY entries.is_dir DESC, entries.path LIMIT ?'''
    with get_db() as conn:
        conn.create_function('name_words', 1, lambda path: words(storage.name_of(path)), deterministic=True)
        rows = conn.execute(sql, (*include, *exclude, *([name] if name else []), SEARCH_LIMIT + 1)).fetchall()
        items = tags.attach(conn, index.describe(conn, rows[:SEARCH_LIMIT]))
    return {'items': items, 'truncated': len(rows) > SEARCH_LIMIT}
