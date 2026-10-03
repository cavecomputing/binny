"""The one place a client-supplied path becomes a real one, plus the rules for naming new files."""
import os
import re
import threading
import uuid
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

# The most a file or folder name can take on disk (Linux and most filesystems).
MAX_NAME_BYTES = 255

# A file being uploaded or downloaded, hidden until it's complete (see partial_in()).
PARTIAL = re.compile(r'\.binny-[0-9a-f]{32}\.part')


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
    if len(name.encode()) > MAX_NAME_BYTES:
        raise InvalidPath('That name is too long')
    return name


def numbered(name, is_dir=False):
    """name, then 'name (1).ext', 'name (2).ext' and so on, without end. A long name loses the end of
    its stem to make room for the number."""
    stem, ext = name, ''
    if not is_dir:
        ext = next((e for e in DOUBLE_EXTENSIONS if name.lower().endswith(e)), None) or os.path.splitext(name)[1]
        stem = name[:len(name) - len(ext)] if ext else name
    yield name
    n = 1
    while True:
        number = f' ({n}){ext}'
        yield shortened(stem, MAX_NAME_BYTES - len(number.encode())) + number
        n += 1


def shortened(text, max_bytes):
    """text cut to at most max_bytes of UTF-8, never inside a character."""
    return text.encode()[:max(max_bytes, 0)].decode(errors='ignore')


def free_name(folder: Path, name, is_dir=False):
    """The first of numbered(name) not taken in folder. Hold NAME_LOCK."""
    return next(candidate for candidate in numbered(name, is_dir) if not os.path.lexists(folder / candidate))


def partial_in(folder: Path):
    """A new hidden file in folder to write into, then rename into place once it's complete, so a
    half-written file never shows under its real name. Being in the same folder keeps the rename on
    one filesystem. index.py deletes one that is left behind."""
    return folder / f'.binny-{uuid.uuid4().hex}.part'


def make_folders(path):
    """Create the missing folders of a cleaned path, top first. Returns the ones it created.

    Raises InvalidPath when a file stands where a folder should be.
    """
    created, current = [], ''
    for part in path.split('/') if path else []:
        current = child(current, part)
        folder = resolve(current)
        try:
            folder.mkdir()
            created.append(current)
        except FileExistsError:
            if folder.is_symlink() or not folder.is_dir():
                raise InvalidPath(f'"{part}" is a file, not a folder') from None
    return created


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
