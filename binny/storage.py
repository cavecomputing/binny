"""The one place a client-supplied path becomes a real one, plus the rules for naming new files."""
import os
import threading
from pathlib import Path, PurePosixPath

from . import config

# Held while picking a name and renaming into it, so two writers never claim the same name.
NAME_LOCK = threading.Lock()

# Kinds the explorer gives their own icon, by extension. Anything else is a plain 'file'.
KINDS = {
    'image': {'.png', '.jpg', '.jpeg', '.gif', '.webp', '.avif', '.bmp', '.svg', '.heic', '.tif', '.tiff', '.ico'},
    'video': {'.mp4', '.mkv', '.webm', '.mov', '.avi', '.m4v', '.wmv', '.flv', '.mpg', '.mpeg'},
    'audio': {'.mp3', '.flac', '.wav', '.ogg', '.opus', '.m4a', '.aac', '.wma'},
    'archive': {'.zip', '.tar', '.gz', '.tgz', '.bz2', '.xz', '.zst', '.7z', '.rar', '.iso', '.img', '.dmg', '.deb', '.rpm'},
    'code': {'.py', '.js', '.html', '.css', '.json', '.yaml', '.yml', '.toml', '.sh', '.c', '.h', '.cpp', '.rs', '.go', '.java', '.rb', '.php', '.sql', '.xml'},
    'doc': {'.pdf', '.txt', '.md', '.doc', '.docx', '.odt', '.rtf', '.xls', '.xlsx', '.ods', '.csv', '.ppt', '.pptx', '.odp', '.epub', '.log'},
}
KIND_BY_EXTENSION = {ext: kind for kind, extensions in KINDS.items() for ext in extensions}

# Kept whole when 'name (1)' is slotted in front of an extension.
DOUBLE_EXTENSIONS = ('.tar.gz', '.tar.bz2', '.tar.xz', '.tar.zst')


class InvalidPath(ValueError):
    """A path or name from the client that Binny refuses. The message is shown to the user."""


def clean_path(user_path):
    """A client path normalized to 'a/b' ('' for the top folder).

    Refuses '..', NUL and any hidden part: Binny keeps its own things (.trash, partial uploads)
    under dot-names, so no client path may reach them.
    """
    if not isinstance(user_path, str) or '\0' in user_path:
        raise InvalidPath('Invalid path')
    parts = [part for part in user_path.split('/') if part]
    if any(part.startswith('.') for part in parts):
        raise InvalidPath('Invalid path')
    return '/'.join(parts)


def resolve(rel):
    """The absolute path of a cleaned path, refusing one that a symlink leads outside data/files/."""
    path = config.FILES_DIR / rel
    root = config.FILES_DIR.resolve()
    real = path.resolve()
    if real != root and not real.is_relative_to(root):
        raise InvalidPath('Invalid path')
    return path


def check_name(name):
    """A new file or folder name from the client, stripped of surrounding spaces."""
    if not isinstance(name, str) or not name.strip():
        raise InvalidPath('A name is required')
    name = name.strip()
    if '/' in name or any(ord(c) < 32 for c in name):
        raise InvalidPath('Names can\'t contain "/" or control characters')
    if name.startswith('.'):
        raise InvalidPath('Names can\'t start with "." (Binny hides those)')
    if len(name.encode()) > 255:
        raise InvalidPath('That name is too long')
    return name


def free_name(folder: Path, name, is_dir=False):
    """name, or 'name (1).ext', 'name (2).ext' and so on: the first not taken in folder. Hold NAME_LOCK."""
    stem, ext = name, ''
    if not is_dir:
        ext = next((e for e in DOUBLE_EXTENSIONS if name.lower().endswith(e)), None) or os.path.splitext(name)[1]
        stem = name[:len(name) - len(ext)] if ext else name
    candidate, n = name, 1
    while os.path.lexists(folder / candidate):
        candidate = f'{stem} ({n}){ext}'
        n += 1
    return candidate


def child(folder, name):
    return f'{folder}/{name}' if folder else name


def parent_of(rel):
    return str(PurePosixPath(rel).parent).removeprefix('.')


def name_of(rel):
    return rel.rsplit('/', 1)[-1]


def kind_of(name, is_dir):
    if is_dir:
        return 'folder'
    return KIND_BY_EXTENSION.get(os.path.splitext(name)[1].lower(), 'file')
