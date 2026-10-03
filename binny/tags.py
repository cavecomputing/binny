"""Tags: a row per (path, tag), keyed by path like the index.

Tags belong to whatever is at a path. The app carries them along when it renames, moves or trashes
something (move()) and clears them where it puts something new (drop()). An item moved or deleted
outside the app leaves its tags at the old path, where nothing shows them, since listings and
searches only see tags of indexed paths; they come back if it does.
"""
import json
import re

from .index import SUBTREE, subtree_args

# Whitespace and commas separate tags when typed, ">" renames one, and a leading "-", "+" or "@"
# is an operator in the tag syntax (see static/js/tags.js), so none of them can be in a tag.
FORBIDDEN = re.compile(r'[\s,>\x00-\x1f\x7f]')
MAX_LENGTH = 100


class InvalidTag(ValueError):
    """A tag from the client that Binny refuses. The message is shown to the user."""


def clean(tag):
    """A tag from the client, trimmed and lowercased."""
    if not isinstance(tag, str) or not tag.strip():
        raise InvalidTag('A tag can\'t be empty')
    tag = tag.strip().lower()
    if len(tag) > MAX_LENGTH:
        raise InvalidTag(f'A tag can be at most {MAX_LENGTH} characters')
    if FORBIDDEN.search(tag):
        raise InvalidTag(f'"{tag}" can\'t be a tag: no spaces, commas or ">"')
    if tag[0] in '-+@':
        raise InvalidTag(f'A tag can\'t start with "{tag[0]}"')
    return tag


def of(conn, paths):
    """{path: its tags, sorted} for each of paths."""
    found = {path: [] for path in paths}
    for row in conn.execute('SELECT path, tag FROM tags WHERE path IN (SELECT value FROM json_each(?)) ORDER BY tag',
                            (json.dumps(list(found)),)):
        found[row['path']].append(row['tag'])
    return found


def attach(conn, items):
    """Give each listed item (a dict with 'path') its tags."""
    tagged = of(conn, [item['path'] for item in items])
    for item in items:
        item['tags'] = tagged[item['path']]
    return items


def move(conn, old, new):
    """Carry the tags of old and everything below it over to new, after the disk move. Tags already at new are stale."""
    drop(conn, new)
    conn.execute(f'UPDATE tags SET path = ? || substr(path, ?) WHERE {SUBTREE}', (new, len(old) + 1, *subtree_args(old)))


def drop(conn, path):
    """Forget the tags of path and everything below it."""
    conn.execute(f'DELETE FROM tags WHERE {SUBTREE}', subtree_args(path))
